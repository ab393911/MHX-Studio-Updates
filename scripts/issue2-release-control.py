"""One-shot release controller for exact Windows-verified Issue2 bytes.
Only the explicitly accepted first-hop manual-recovery criterion is waived.
Never writes updates/preview.json and never rebuilds the application.
"""
import base64
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

REPO='ab393911/MHX-Studio-Updates'
SOURCE='25b5adcad0221cd3f6e1c90d569b8006ea4ebcc7'
VERSION='3.5.0-preview.41-issue2.2'
SHA='28a5f525f9df3c04d53818dc561cde21765c3850f7f3c5aea9182566daebd9a7'
RUN='41629992043'
PRIOR_VERSION='3.5.0-preview.41-issue2.1'
PRIOR_SHA='b7ac42b0800b96c4dd60a4245e31ebf054373675ae23702af4bb7ba978bf3cb7'
STABLE_BLOB='fc0cb8c83a5c3aad7df831354b930fe82c4fb013'
TAG='issue2-v'+VERSION
CHANNEL='https://raw.githubusercontent.com/'+REPO+'/updates/issue2-test.json'
ASSET='https://github.com/'+REPO+'/releases/download/'+TAG+'/update.json'

def call(*args):
    return subprocess.run(args,check=True,capture_output=True,text=True).stdout

def api(path,body=None):
    args=['gh','api',path]
    if body is not None:
        request=Path(os.environ['RUNNER_TEMP'])/'issue2-api-request.json'
        request.write_text(json.dumps(body))
        args+=['--method','PUT','--input',str(request)]
    return json.loads(call(*args))

def load(p):return json.loads(Path(p).read_text(encoding='utf-8-sig'))
def save(p,obj):Path(p).write_text(json.dumps(obj,ensure_ascii=False,indent=2)+'\n')
def require(condition,message):
    if not condition:raise RuntimeError(message)

UPGRADE_KEYS=['passed','one_click_without_confirmation','new_gui_visible','configuration_byte_identical','calibration_byte_identical','studio_database_preserved','studio_data_seeded','role_execution_history_preserved','game_settings_byte_preserved','old_exe_backup_retained','configuration_snapshot_verified','previous_update_backup_preserved','progress_history_logs_preserved','emulator_directory_sentinels_preserved','sha256_verified','real_update_button_clicked']
GUI_KEYS=['passed','native_windows','gui_diagnostic_report_visible_and_clipboard_verified','gui_original_image_view_exact_pixels','zip_original_png_and_report_sha_verified','gui_zip_export_without_file_hunting','gui_rule_edit_confirmed_and_persisted','gui_history_preview_is_read_only','gui_history_restore_confirmed_and_backed_up','restored_rules_work_after_restart','thresholds_unchanged','unknown_frame_rejected_without_click']
SCENARIOS=['success','startup_timeout','immediate_exit','config_migration_exit','sha_tamper','sharing_violation','interrupted_recovery']

def validate_upgrade(d,baseline):
    for key in UPGRADE_KEYS:
        require(d.get(key) is True,'Upgrade gate failed: '+key)
    require(d['source_commit']==SOURCE and d['version']==VERSION and d['previous_version']==baseline and d['confirmation_clicks']==0,'Upgrade source/baseline/version/confirmation mismatch')

def validate_rollback(n):
    require(n['passed'] is True and n['gui_smoke']['source_commit']==SOURCE and n['gui_smoke']['version']==VERSION,'Native report mismatch')
    for scenario in SCENARIOS:
        r=n[scenario]
        require(r['passed'] and r['config_unchanged'] and r['studio_order_database_preserved'],'Native rollback/preflight failed: '+scenario)
    require(n['interrupted_recovery']['recovery_helper_restarted_by_test'] is True and n['interrupted_recovery']['automatic_without_restarting_helper'] is False,'Unexpected manual interruption scope')

def validate_bytes(c,b):
    require(c['source_commit']==SOURCE and c['version']==VERSION and c['sha256']==SHA and c['isolated_test_id']=='','Candidate metadata mismatch')
    require(hashlib.sha256(b).hexdigest()==SHA and len(b)==c['size']==8514048,'Candidate bytes differ from frozen native artifact')

def validate():
    run=api(f'repos/{REPO}/actions/runs/{RUN}')
    require(run['head_sha']==SOURCE and run['conclusion']=='success','Exact native verification run did not succeed')
    call('git','diff','--exit-code',SOURCE,'HEAD','--','cmd','internal','go.mod','go.sum')
    c=load('dist/candidate.json');n=load('dist/issue2-native-update.json');g=load('dist/recognition-tests.json')
    validate_bytes(c,Path('dist/MHXStudio.exe').read_bytes())
    validate_rollback(n)
    validate_upgrade(n['preview40_direct_gui_upgrade'],'3.5.0-preview.40')
    validate_upgrade(n['preview41_issue2_gui_upgrade'],PRIOR_VERSION)
    for key in GUI_KEYS:require(g.get(key) is True,'Frozen GUI gate failed: '+key)
    failures=[];counts={}
    for line in Path('dist/windows-unit-tests.json').read_text(encoding='utf-8-sig').splitlines():
        try:e=json.loads(line)
        except json.JSONDecodeError:continue
        if e.get('Test') and e.get('Action') in ['pass','fail','skip']:
            counts[e['Action']]=counts.get(e['Action'],0)+1
            if e['Action']=='fail':failures.append(e)
    require(not failures and counts.get('pass',0)==271,'Windows unit results failed or incomplete')
    stable=api(f'repos/{REPO}/contents/preview.json?ref=updates')
    previous=api(f'repos/{REPO}/contents/issue2-test.json?ref=updates')
    prior=json.loads(base64.b64decode(previous['content']))
    require(stable['sha']==STABLE_BLOB,'Default manifest changed since authorization')
    require(prior['version']==PRIOR_VERSION and prior['sha256']==PRIOR_SHA,'Test channel changed since authorization')
    save('dist/channel-before.json',{'stable_blob':stable['sha'],'test_blob':previous['sha'],'test_content':previous['content']})
    save('dist/ISSUE2-VERIFICATION.json',{'source_commit':SOURCE,'version':VERSION,'sha256':SHA,'native_run':run['html_url'],'latest_business_diff_empty':True,'windows_unit_tests':counts,'excluded_experimental_test':'TestNativeWindowsAssetAmounts','native_update':n,'diagnostic_gui':g,'release_policy':{'user_accepted_helper_forced_interruption_manual_recovery':True,'acceptance_date_shanghai':'2026-11-03','scope':'recognition diagnostics only; no real-game fix claim; no runtime changes or deliberate mistakes'},'real_ldplayer_tested':False,'workflow_source_commit':os.environ['GITHUB_SHA']})

def verify_online(url,report):
    # Fail immediately on any transport, metadata, PE or integrity failure.
    print(call('go','run','./cmd/release','-verify',url,'-commit',SOURCE,'-version',VERSION,'-report',report))
    require(load(report)['sha256']==SHA,'Online bytes mismatch')

def publish():
    validate()
    # Packaging uses the existing release utility and the exact native artifact.
    print(call('go','run','./cmd/release','-exe','dist/MHXStudio.exe','-version',VERSION,'-commit',SOURCE,'-report','dist/issue2-native-update.json'))
    m=load('dist/update.json')
    m['download_url']=f'https://github.com/{REPO}/releases/download/{TAG}/MHXStudio.exe'
    m['notes']='识别诊断测试版：一键查看/复制识别报告、查看960×540原图、导出ZIP与规则历史恢复。复用已验证EXE，保留全部现有功能、配置、SQLite、绑定、角色历史及备份回滚；不新增游戏任务，不涉及金额。真实主界面识别失败根因仍待实机诊断，不宣称修复。更新助手被强制中断时可能需要在资源管理器双击原事务updater.exe并确认恢复，不承诺无人干预恢复。'
    save('dist/update.json',m)
    Path('dist/ISSUE2-RECOVERY.md').write_bytes(Path('docs/ISSUE_2_TEST_CHANNEL_RECOVERY.md').read_bytes())
    notes=f"""MHX Studio {VERSION} 识别诊断测试版

源码 {SOURCE}；复用Windows原生验证运行 {RUN} 的原EXE，SHA-256 {SHA}。未重建业务EXE。

交付一键识别诊断、报告复制、原图查看、ZIP导出及规则管理。保留现有业务、职业/角色功能、随机短延迟与点击偏移；不新增随机误操作、不执行游戏任务、不涉及金额。不宣称已修复真实主界面识别失败。

Windows真实GUI及preview.40、issue2.1正常升级、配置/SQLite保留、SHA拒绝与失败回滚自动测试通过；真实雷电仍待验收。公开资产升级复核通过后才推进测试清单，默认preview.json不变。

已加入 {CHANNEL} 的issue2.1用户，通道验证完成后只点击“一键检查更新”。无需下载ZIP、替换EXE、CMD/BAT或Python。

更新助手被强制中断仍可能需要人工GUI恢复：原安装目录.mhx-updates/tx-.../updater.exe双击后确认；旧EXE和配置/SQLite快照在.mhx-backups。不得删除目标、手工覆盖或清空数据，详见附件ISSUE2-RECOVERY.md。
"""
    Path('dist/ISSUE2-RELEASE.md').write_text(notes)
    created=False
    try:
        print(call('gh','release','create',TAG,'dist/MHXStudio.exe','dist/update.json','dist/SHA256SUMS.txt','dist/ISSUE2-VERIFICATION.json','dist/issue2-native-update.json','dist/recognition-tests.json','dist/ISSUE2-RECOVERY.md','--repo',REPO,'--target',SOURCE,'--title',f'MHX Studio {VERSION} — Issue2 test','--notes-file','dist/ISSUE2-RELEASE.md','--prerelease','--draft','--latest=false'))
        created=True
        draft=Path(os.environ['RUNNER_TEMP'])/'issue2-draft-download'
        print(call('gh','release','download',TAG,'--repo',REPO,'--pattern','MHXStudio.exe','--dir',str(draft)))
        require(hashlib.sha256((draft/'MHXStudio.exe').read_bytes()).hexdigest()==SHA,'Draft online asset hash mismatch')
        print(call('gh','release','edit',TAG,'--repo',REPO,'--draft=false','--latest=false'))
        verify_online(ASSET,'dist/public-download.json')
    except Exception:
        if created:call('gh','release','edit',TAG,'--repo',REPO,'--draft=true')
        raise

def promote():
    m=load('dist/update.json');d=load('reports/published-gui-update.json');recovery=load('reports/manual-recovery.json')
    require(m['source_commit']==SOURCE and m['version']==VERSION and m['sha256']==SHA,'Manifest mismatch')
    validate_upgrade(d,PRIOR_VERSION)
    validate_rollback(load('reports/public-rollback.json'))
    for key in ['passed','old_exe_restored','old_backup_retained','config_bytes_preserved','sqlite_order_preserved','restored_gui_visible','no_cmd_bat_python_or_manual_exe_copy']:
        require(recovery[key] is True,'Manual GUI recovery failed: '+key)
    require(recovery['baseline_version']==PRIOR_VERSION and recovery['baseline_sha256']==PRIOR_SHA,'Recovery tested the wrong installed version')
    require(load('dist/public-download.json')['sha256']==SHA,'Public download evidence mismatch')
    release=api(f'repos/{REPO}/releases/tags/{TAG}')
    require(not release['draft'] and release['prerelease'] and release['target_commitish']==SOURCE,'Release metadata mismatch')
    stable=api(f'repos/{REPO}/contents/preview.json?ref=updates')
    endpoint=f'repos/{REPO}/contents/issue2-test.json'
    previous=api(endpoint+'?ref=updates')
    before=load('dist/channel-before.json')
    require(stable['sha']==before['stable_blob']==STABLE_BLOB,'Default manifest changed; refusing promotion')
    require(previous['sha']==before['test_blob'],'Concurrent test-channel change; refusing promotion')
    body={'branch':'updates','message':f'Publish verified Issue2 test channel {VERSION}','content':base64.b64encode(Path('dist/update.json').read_bytes()).decode()}
    if previous:body['sha']=previous['sha']
    updated=api(endpoint,body)
    try:
        require(api(f'repos/{REPO}/contents/preview.json?ref=updates')['sha']==stable['sha'],'Default manifest changed during promotion')
        verify_online(CHANNEL,'reports/test-channel-download.json')
        result={'published':True,'version':VERSION,'source_commit':SOURCE,'sha256':SHA,'test_manifest':CHANNEL,'release_manifest':ASSET,'public_gui_update':d,'manual_gui_recovery':recovery,'public_rollback':load('reports/public-rollback.json'),'stable_manifest_blob_before':stable['sha'],'stable_manifest_blob_after':api(f'repos/{REPO}/contents/preview.json?ref=updates')['sha'],'default_preview_manifest_unchanged':True,'legacy_first_hop_manual_recovery_risk_accepted':True,'real_ldplayer_tested':False,'workflow_source_commit':os.environ['GITHUB_SHA'],'run_id':os.environ['GITHUB_RUN_ID'],'channel_commit':updated['commit']['sha'],'release_tag':TAG}
        save('reports/issue2-publication.json',result)
        print(call('gh','release','upload',TAG,'reports/published-gui-update.json','reports/manual-recovery.json','reports/public-rollback.json','reports/test-channel-download.json','reports/issue2-publication.json','--repo',REPO))
    except Exception:
        current=api(endpoint+'?ref=updates')
        if current['sha']==updated['content']['sha']:
            if previous:
                api(endpoint,{'branch':'updates','sha':current['sha'],'message':'Restore prior Issue2 test channel after verification failure','content':previous['content'].replace('\n','')})
            else:
                request=Path(os.environ['RUNNER_TEMP'])/'delete-issue2-channel.json'
                request.write_text(json.dumps({'branch':'updates','sha':current['sha'],'message':'Remove unverified Issue2 test channel'}))
                call('gh','api',endpoint,'--method','DELETE','--input',str(request))
        raise


if __name__=='__main__':
    mode=sys.argv[1]
    if mode=='publish':publish()
    elif mode=='promote':promote()
    else:raise RuntimeError('Unknown mode')
