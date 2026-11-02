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
import time

REPO='ab393911/MHX-Studio-Updates'
SOURCE='f3bc322408aaa92fcc91d8fac0133144238d8184'
VERSION='3.5.0-preview.41-issue2.1'
SHA='b7ac42b0800b96c4dd60a4245e31ebf054373675ae23702af4bb7ba978bf3cb7'
RUN='41438510528'
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

def validate():
    run=api(f'repos/{REPO}/actions/runs/{RUN}')
    require(run['head_sha']==SOURCE and run['conclusion']=='success','Exact historical verification run did not succeed')
    call('git','diff','--exit-code',SOURCE,'HEAD','--','cmd','internal','go.mod','go.sum')
    c=load('dist/candidate.json');n=load('dist/issue2-native-update.json')
    b=Path('dist/MHXStudio.exe').read_bytes()
    require(c['source_commit']==SOURCE and c['version']==VERSION and c['sha256']==SHA and c['isolated_test_id']=='','Candidate metadata mismatch')
    require(hashlib.sha256(b).hexdigest()==SHA and len(b)==c['size'],'Candidate bytes differ from native tested artifact')
    require(n['passed'] is True and n['gui_smoke']['source_commit']==SOURCE,'Native report mismatch')
    for scenario in ['success','startup_timeout','immediate_exit','config_migration_exit','sha_tamper','sharing_violation','interrupted_recovery']:
        require(n[scenario]['passed'] and n[scenario]['config_unchanged'] and n[scenario]['studio_order_database_preserved'],'Native scenario failed: '+scenario)
    require(n['interrupted_recovery']['recovery_helper_restarted_by_test'] is True and n['interrupted_recovery']['automatic_without_restarting_helper'] is False,'Unexpected legacy interruption scope')
    d=n['preview40_direct_gui_upgrade']
    for key in ['passed','one_click_without_confirmation','configuration_byte_identical','studio_database_preserved','game_settings_byte_preserved','role_execution_history_preserved','previous_update_backup_preserved','progress_history_logs_preserved','emulator_directory_sentinels_preserved','new_gui_visible','test_channel_enrolled_via_preview40_gui']:
        require(d[key] is True,'Published preview40 upgrade gate failed: '+key)
    require(d['previous_version']=='3.5.0-preview.40' and d['source_commit']==SOURCE and d['confirmation_clicks']==0,'Baseline/source/confirmation mismatch')
    failures=[];counts={}
    for line in Path('dist/windows-unit-tests.json').read_text(encoding='utf-8-sig').splitlines():
        try:e=json.loads(line)
        except json.JSONDecodeError:continue
        if e.get('Test') and e.get('Action') in ['pass','fail','skip']:
            counts[e['Action']]=counts.get(e['Action'],0)+1
            if e['Action']=='fail':failures.append(e)
    require(not failures and counts.get('pass',0)>=256,'Windows unit results failed or incomplete')
    save('dist/ISSUE2-VERIFICATION.json',{'source_commit':SOURCE,'version':VERSION,'sha256':SHA,'native_run':run['html_url'],'latest_business_diff_empty':True,'windows_unit_tests':counts,'native_update':n,'release_policy':{'user_accepted_legacy_first_hop_manual_recovery':True,'acceptance_date_shanghai':'2026-11-02','waiver_scope':'only helper forcibly terminated during legacy first hop; all other gates remain mandatory'},'real_ldplayer_tested':False,'workflow_source_commit':os.environ['GITHUB_SHA']})

def verify_online(url,report):
    for attempt in range(6):
        try:
            print(call('go','run','./cmd/release','-verify',url,'-commit',SOURCE,'-version',VERSION,'-report',report))
            require(load(report)['sha256']==SHA,'Online bytes mismatch')
            return
        except subprocess.CalledProcessError:
            if attempt==5:raise
            time.sleep(5)  # bounded propagation wait after release/contents API write

def publish():
    validate()
    # Packaging uses the existing release utility and the exact native artifact.
    print(call('go','run','./cmd/release','-exe','dist/MHXStudio.exe','-version',VERSION,'-commit',SOURCE,'-report','dist/issue2-native-update.json'))
    m=load('dist/update.json')
    m['download_url']=f'https://github.com/{REPO}/releases/download/{TAG}/MHXStudio.exe'
    m['notes']='Issue2独立候选测试版：修复游戏绑定复用、ADB掉线身份重验、停止/切换实例后的异步回调及错误点击重放保护。保留现有业务、配置、SQLite和备份回滚。preview.40正常一键升级与启动失败回滚已通过Windows自动测试；首次升级助手若被强制终止，可能需在资源管理器双击原事务updater.exe并确认恢复。用户已接受此剩余风险；不是无人干预中断恢复。真实雷电/游戏尚待分阶段验收，不自动执行本轮游戏验收任务。'
    save('dist/update.json',m)
    Path('dist/ISSUE2-RECOVERY.md').write_bytes(Path('docs/ISSUE_2_TEST_CHANNEL_RECOVERY.md').read_bytes())
    notes=f'''Issue2 独立在线候选 {VERSION}\n\n业务提交 {SOURCE}；复用 Windows 原生验证构建，不重建、不恢复已删除功能。\n\nWindows 原版 preview.40 一次点击升级、SHA校验、替换前备份、自动重启可见GUI和启动失败回滚通过。仅接受旧助手被强制终止时可能需要人工GUI恢复的剩余风险，不宣称无人干预恢复。\n\n首次在软件“更新清单”填写 {CHANNEL}，点“保存设置”，再点“一键检查更新”；以后只点检查。默认 preview.json 不变。请等测试通道验证完成再加入。\n\n无需下载ZIP、替换EXE、CMD/BAT或Python。恢复方法见附件 ISSUE2-RECOVERY.md：原安装目录 .mhx-updates/tx-.../updater.exe 双击后确认；旧EXE及配置/SQLite快照在 .mhx-backups，不要手工复制覆盖。目标存在或校验失败时停止操作并保留文件。\n\n真实雷电尚未验收；CI仅执行更新/恢复和接口替身检查，没有执行真实游戏任务。\n'''
    Path('dist/ISSUE2-RELEASE.md').write_text(notes)
    created=False
    try:
        print(call('gh','release','create',TAG,'dist/MHXStudio.exe','dist/update.json','dist/SHA256SUMS.txt','dist/ISSUE2-VERIFICATION.json','dist/issue2-native-update.json','dist/ISSUE2-RECOVERY.md','--repo',REPO,'--target',SOURCE,'--title',f'MHX Studio {VERSION} — Issue2 test','--notes-file','dist/ISSUE2-RELEASE.md','--prerelease','--draft','--latest=false'))
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
    for key in ['passed','one_click_without_confirmation','new_gui_visible','configuration_byte_identical','studio_database_preserved','role_execution_history_preserved','game_settings_byte_preserved','previous_update_backup_preserved','progress_history_logs_preserved','emulator_directory_sentinels_preserved']:
        require(d[key] is True,'Public GUI gate failed: '+key)
    require(d['source_commit']==SOURCE and d['version']==VERSION and d['previous_version']=='3.5.0-preview.40','Public source/baseline mismatch')
    for key in ['passed','old_exe_restored','old_backup_retained','config_bytes_preserved','sqlite_order_preserved','restored_gui_visible','no_cmd_bat_python_or_manual_exe_copy']:
        require(recovery[key] is True,'Manual GUI recovery failed: '+key)
    release=api(f'repos/{REPO}/releases/tags/{TAG}')
    require(not release['draft'] and release['prerelease'] and release['target_commitish']==SOURCE,'Release metadata mismatch')
    stable=api(f'repos/{REPO}/contents/preview.json?ref=updates')
    endpoint=f'repos/{REPO}/contents/issue2-test.json'
    try:previous=api(endpoint+'?ref=updates')
    except subprocess.CalledProcessError as exc:
        require('404' in exc.stderr,'Cannot determine previous test channel');previous=None
    body={'branch':'updates','message':f'Publish verified Issue2 test channel {VERSION}','content':base64.b64encode(Path('dist/update.json').read_bytes()).decode()}
    if previous:body['sha']=previous['sha']
    updated=api(endpoint,body)
    try:
        require(api(f'repos/{REPO}/contents/preview.json?ref=updates')['sha']==stable['sha'],'Default manifest changed during promotion')
        verify_online(CHANNEL,'reports/test-channel-download.json')
        result={'published':True,'version':VERSION,'source_commit':SOURCE,'sha256':SHA,'test_manifest':CHANNEL,'release_manifest':ASSET,'public_gui_update':d,'manual_gui_recovery':recovery,'stable_manifest_blob_before':stable['sha'],'stable_manifest_blob_after':api(f'repos/{REPO}/contents/preview.json?ref=updates')['sha'],'default_preview_manifest_unchanged':True,'legacy_first_hop_manual_recovery_risk_accepted':True,'real_ldplayer_tested':False,'workflow_source_commit':os.environ['GITHUB_SHA'],'run_id':os.environ['GITHUB_RUN_ID']}
        save('reports/issue2-publication.json',result)
        print(call('gh','release','upload',TAG,'reports/published-gui-update.json','reports/manual-recovery.json','reports/test-channel-download.json','reports/issue2-publication.json','--repo',REPO))
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
