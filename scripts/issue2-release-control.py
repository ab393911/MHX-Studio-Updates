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
SOURCE='461cac4f5436b0989b6b1af5e0a51b6fa32660ca'
VERIFICATION='84caf61b0d906b9943c06c4fbcec537848f3f6dd'
SIZE=8585728
VERSION='3.5.0-preview.41-issue2.3'
SHA='eab8c4b55bf28fe5a51b0030ff6fbc930403fa2c2c01b9cb7b7490d39e1d5dad'
RUN='41838190565'
PRIOR_VERSION='3.5.0-preview.41-issue2.2'
PRIOR_SHA='28a5f525f9df3c04d53818dc561cde21765c3850f7f3c5aea9182566daebd9a7'
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

UPGRADE_KEYS=['passed','one_click_without_confirmation','new_gui_visible','configuration_byte_identical','calibration_byte_identical','studio_database_preserved','studio_data_seeded','role_execution_history_preserved','game_settings_byte_preserved','old_exe_backup_retained','configuration_snapshot_verified','previous_update_backup_preserved','progress_history_logs_preserved','emulator_directory_sentinels_preserved','sha256_verified','real_update_button_clicked','all_five_calibrations_and_shared_rules_preserved','icon_draft_sentinel_preserved']
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
    require(c['source_commit']==SOURCE and c['version']==VERSION and c['sha256']==SHA and c['isolated_test_id']=='' and c.get('verification_commit')==VERIFICATION,'Candidate metadata mismatch')
    require(hashlib.sha256(b).hexdigest()==SHA and len(b)==c['size']==SIZE,'Candidate bytes differ from frozen native artifact')

def validate():
    run=api(f'repos/{REPO}/actions/runs/{RUN}')
    require(run['head_sha']==VERIFICATION and run['conclusion']=='success','Exact native verification run did not succeed')
    changes=call('git','diff','--name-only',SOURCE,'HEAD','--','cmd/mhxstudio','internal','go.mod','go.sum').splitlines()
    require(all(name.endswith('_test.go') for name in changes),'Business runtime changed after frozen verification')
    c=load('dist/candidate.json');n=load('dist/issue2-native-update.json');g=load('dist/recognition-tests.json')
    validate_bytes(c,Path('dist/MHXStudio.exe').read_bytes())
    validate_rollback(n)
    validate_upgrade(n['preview40_direct_gui_upgrade'],'3.5.0-preview.40')
    validate_upgrade(n['preview41_issue2_gui_upgrade'],'3.5.0-preview.41-issue2.1')
    validate_upgrade(n['preview41_issue2_2_gui_upgrade'],PRIOR_VERSION)
    for key in GUI_KEYS:require(g.get(key) is True,'Frozen GUI gate failed: '+key)
    require(g.get('icon_draft_gui_tested') is True,'Icon draft GUI was not exercised')
    redacted=load('dist/redacted-png/recognition-tests.json')
    for key in GUI_KEYS:require(redacted.get(key) is True,'Real redacted PNG GUI gate failed: '+key)
    require(redacted.get('redacted_png_tested') is True and redacted.get('private_original_png_tested') is False and redacted.get('source_png_sha256')=='e7e2e9436a79f2535e3955a3961e0317e5308672d9d8536ecab724aabc16444e','Wrong PNG provenance')
    require(redacted.get('calibration_preview_gdi_handles')==[9,9,9] and redacted.get('viewer_reopen_gdi_handles')==[10,10,10],'PNG resource lifecycle incomplete')
    embedded=load('dist/embedded-metadata.json')
    require(embedded['version']==VERSION and embedded['source_commit']==SOURCE,'Embedded metadata mismatch')
    failures=[];counts={}
    for line in Path('dist/windows-unit-tests.json').read_text(encoding='utf-8-sig').splitlines():
        try:e=json.loads(line)
        except json.JSONDecodeError:continue
        if e.get('Test') and e.get('Action') in ['pass','fail','skip']:
            counts[e['Action']]=counts.get(e['Action'],0)+1
            if e['Action']=='fail':failures.append(e)
    require(not failures and counts.get('pass',0)==285 and counts.get('skip',0)==3,'Windows unit results failed or incomplete')
    portable_counts={}
    for line in Path('portable/portable-tests.json').read_text(encoding='utf-8-sig').splitlines():
        e=json.loads(line)
        if e.get('Test') and e.get('Action') in ['pass','fail','skip']:portable_counts[e['Action']]=portable_counts.get(e['Action'],0)+1
    require(portable_counts=={'pass':282,'skip':3},'Frozen Linux race report incomplete or failed')
    stable=api(f'repos/{REPO}/contents/preview.json?ref=updates')
    previous=api(f'repos/{REPO}/contents/issue2-test.json?ref=updates')
    prior=json.loads(base64.b64decode(previous['content']))
    require(stable['sha']==STABLE_BLOB,'Default manifest changed since authorization')
    require(prior['version']==PRIOR_VERSION and prior['sha256']==PRIOR_SHA,'Test channel changed since authorization')
    save('dist/channel-before.json',{'stable_blob':stable['sha'],'test_blob':previous['sha'],'test_content':previous['content']})
    save('dist/ISSUE2-VERIFICATION.json',{'source_commit':SOURCE,'version':VERSION,'sha256':SHA,'native_run':run['html_url'],'latest_business_diff_empty':True,'windows_unit_tests':counts,'linux_race_tests':portable_counts,'verification_commit':VERIFICATION,'redacted_png_gui':redacted,'excluded_experimental_test':'TestNativeWindowsAssetAmounts','native_update':n,'diagnostic_gui':g,'release_policy':{'user_accepted_helper_forced_interruption_manual_recovery':True,'acceptance_date_shanghai':'2026-11-04','scope':'image display and read-only icon diagnostics; no real-device fix claim; frozen binary only'},'real_ldplayer_tested':False,'workflow_source_commit':os.environ['GITHUB_SHA']})

def verify_online(url,report):
    # Fail immediately on any transport, metadata, PE or integrity failure.
    print(call('go','run','./cmd/release','-verify',url,'-commit',SOURCE,'-version',VERSION,'-report',report))
    r=load(report)
    require(r['sha256']==SHA and r.get('size')==SIZE and r.get('source_commit')==SOURCE and r.get('version')==VERSION,'Online bytes or metadata mismatch')

def publish():
    validate()
    # Packaging uses the existing release utility and the exact native artifact.
    print(call('go','run','./cmd/release','-exe','dist/MHXStudio.exe','-version',VERSION,'-commit',SOURCE,'-report','dist/issue2-native-update.json'))
    m=load('dist/update.json')
    m['download_url']=f'https://github.com/{REPO}/releases/download/{TAG}/MHXStudio.exe'
    m['notes']='受控测试版：Win32/GDI图片显示修补及多锚点只读诊断草稿；真实PNG回归通过，用户设备尚未验收。保留一键报告/复制、960×540原图、ZIP导出与规则历史恢复。复用已验证EXE，保留全部现有功能、配置、SQLite、绑定、角色历史及备份回滚；不新增游戏任务，不涉及金额。真实主界面识别失败根因仍待实机诊断，不宣称修复。更新助手被强制中断时可能需要在资源管理器双击原事务updater.exe并确认恢复，不承诺无人干预恢复。'
    save('dist/update.json',m)
    Path('dist/ISSUE2-RECOVERY.md').write_bytes(Path('docs/ISSUE_2_TEST_CHANNEL_RECOVERY.md').read_bytes())
    notes=f"""MHX Studio {VERSION} 图片显示与多锚点诊断受控测试版

源码 {SOURCE}；复用Windows原生验证运行 {RUN} 的原EXE，SHA-256 {SHA}。未重建业务EXE。

交付原图/标定窗口显示修补、多图标独立标定与只读诊断、报告复制、ZIP导出及规则历史。多图标尚不接入游戏点击；没有草稿时明确显示未配置。保留现有业务、职业/角色功能、随机短延迟与点击偏移；不新增随机误操作、不执行游戏任务、不涉及金额。不宣称已修复真实主界面识别失败。

Windows真实脱敏PNG窗口回归、285项Windows/282项Linux及preview.40、issue2.1、issue2.2正常升级、配置/SQLite保留、SHA拒绝与失败回滚自动测试通过；真实雷电仍待验收。公开资产升级复核通过后才推进测试清单，默认preview.json不变。

已加入 {CHANNEL} 的issue2.2用户，通道验证完成后只点击“一键检查更新”。无需下载ZIP、替换EXE、CMD/BAT或Python。

更新助手被强制中断仍可能需要人工GUI恢复：原安装目录.mhx-updates/tx-.../updater.exe双击后确认；旧EXE和配置/SQLite快照在.mhx-backups。不得删除目标、手工覆盖或清空数据，详见附件ISSUE2-RECOVERY.md。
"""
    Path('dist/ISSUE2-RELEASE.md').write_text(notes)
    created=False
    try:
        print(call('gh','release','create',TAG,'dist/MHXStudio.exe','dist/update.json','dist/SHA256SUMS.txt','dist/ISSUE2-VERIFICATION.json','dist/issue2-native-update.json','dist/recognition-tests.json','dist/ISSUE2-RECOVERY.md','--repo',REPO,'--target',SOURCE,'--title',f'MHX Studio {VERSION} — Issue2 test','--notes-file','dist/ISSUE2-RELEASE.md','--prerelease','--draft','--latest=false'))
        created=True
        draft=Path(os.environ['RUNNER_TEMP'])/'issue2-draft-download'
        print(call('gh','release','download',TAG,'--repo',REPO,'--pattern','MHXStudio.exe','--dir',str(draft)))
        downloaded=(draft/'MHXStudio.exe').read_bytes()
        require(hashlib.sha256(downloaded).hexdigest()==SHA and len(downloaded)==SIZE,'Draft online asset hash/size mismatch')
        print(call('gh','release','edit',TAG,'--repo',REPO,'--draft=false','--latest=false'))
        verify_online(ASSET,'dist/public-download.json')
    except Exception:
        if created:call('gh','release','edit',TAG,'--repo',REPO,'--draft=true')
        raise

def validate_transport(d):
    require(d.get('passed') is True and d.get('baseline_version')==PRIOR_VERSION and d.get('baseline_sha256')==PRIOR_SHA and d.get('version')==VERSION and d.get('source_commit')==SOURCE and d.get('sha256')==SHA,'Wrong public transport test inputs')
    for mode in ['http_503','truncated','sha_corruption']:
        for key in ['passed','installed_gui_remained','old_exe_unchanged','config_bindings_jobs_unchanged','five_calibrations_preserved','sqlite_order_preserved','prior_backup_preserved','no_install_transaction']:
            require(d.get('cases',{}).get(mode,{}).get(key) is True,'Download rejection gate failed: '+mode+'/'+key)

def promote():
    m=load('dist/update.json');d=load('reports/published-gui-update.json');recovery=load('reports/manual-recovery.json')
    require(m['source_commit']==SOURCE and m['version']==VERSION and m['sha256']==SHA,'Manifest mismatch')
    validate_upgrade(d,PRIOR_VERSION)
    validate_rollback(load('reports/public-rollback.json'))
    validate_transport(load('reports/transport-failures.json'))
    for key in ['passed','old_exe_restored','old_backup_retained','config_bytes_preserved','sqlite_order_preserved','restored_gui_visible','no_cmd_bat_python_or_manual_exe_copy']:
        require(recovery[key] is True,'Manual GUI recovery failed: '+key)
    require(recovery['baseline_version']==PRIOR_VERSION and recovery['baseline_sha256']==PRIOR_SHA,'Recovery tested the wrong installed version')
    online=load('dist/public-download.json')
    require(online['sha256']==SHA and online.get('size')==SIZE,'Public download evidence mismatch')
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
        result={'published':True,'version':VERSION,'source_commit':SOURCE,'sha256':SHA,'test_manifest':CHANNEL,'release_manifest':ASSET,'public_gui_update':d,'manual_gui_recovery':recovery,'public_rollback':load('reports/public-rollback.json'),'download_rejection':load('reports/transport-failures.json'),'verification_commit':VERIFICATION,'stable_manifest_blob_before':stable['sha'],'stable_manifest_blob_after':api(f'repos/{REPO}/contents/preview.json?ref=updates')['sha'],'default_preview_manifest_unchanged':True,'legacy_first_hop_manual_recovery_risk_accepted':True,'real_ldplayer_tested':False,'workflow_source_commit':os.environ['GITHUB_SHA'],'run_id':os.environ['GITHUB_RUN_ID'],'channel_commit':updated['commit']['sha'],'release_tag':TAG}
        save('reports/issue2-publication.json',result)
        print(call('gh','release','upload',TAG,'reports/published-gui-update.json','reports/manual-recovery.json','reports/public-rollback.json','reports/transport-failures.json','reports/test-channel-download.json','reports/issue2-publication.json','--repo',REPO))
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
