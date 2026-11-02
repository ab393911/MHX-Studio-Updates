"""Persist bounded execution evidence; never create releases or update channels."""
import base64
import json
import os
from pathlib import Path
import subprocess

def gh(*args, **kw):
    return subprocess.run(['gh', 'api', *args], check=True, text=True, capture_output=True, **kw).stdout

report = {key: os.environ[key] for key in ['SOURCE_SHA','RUN_ID','RUN_ATTEMPT','PORTABLE_RESULT','WINDOWS_RESULT']}
report['run_url'] = f"https://github.com/{os.environ['REPOSITORY']}/actions/runs/{report['RUN_ID']}"
report['published'] = False
report['stable_manifest_changed'] = False
report['reports'] = {}
for p in Path('verification-artifacts').rglob('*.json'):
    if p.name in {'candidate.json', 'issue2-native-update.json', 'upgrade.json'}:
        report['reports'][str(p)] = json.loads(p.read_text(encoding='utf-8-sig'))
for p in Path('verification-artifacts').rglob('*tests.json'):
    counts = {}
    for line in p.read_text(encoding='utf-8-sig').splitlines():
        try:
            e = json.loads(line)
        except json.JSONDecodeError:
            continue
        if e.get('Test') and e.get('Action') in {'pass','fail','skip'}:
            counts[e['Action']] = counts.get(e['Action'],0)+1
    report['reports'][str(p)] = counts
for p in Path('verification-artifacts').rglob('*.log'):
    report['reports'][str(p)] = p.read_text(encoding='utf-8-sig',errors='replace')[-24000:]
report['ready_for_publication'] = False
report['release_gate_note'] = 'Requires all native acceptance including unattended interruption recovery; no automatic publication is configured.'
repo = os.environ['REPOSITORY']
if report['WINDOWS_RESULT'] != 'success':
    try:
        jobs = json.loads(gh(f'repos/{repo}/actions/runs/{report["RUN_ID"]}/jobs'))['jobs']
        for job in jobs:
            if job['name'] == 'windows':
                report['failed_windows_log_tail'] = gh(f'repos/{repo}/actions/jobs/{job["id"]}/logs')[-32000:]
    except (subprocess.CalledProcessError, KeyError, json.JSONDecodeError) as exc:
        report['log_retrieval_error'] = type(exc).__name__

branch = 'issue2-ci-evidence'
try:
    gh(f'repos/{repo}/git/ref/heads/{branch}')
except subprocess.CalledProcessError:
    gh(f'repos/{repo}/git/refs', '--method', 'POST', '-f', f'ref=refs/heads/{branch}', '-f', f'sha={report["SOURCE_SHA"]}')
path = 'issue2-verification.json'
body = {'branch':branch,'message':f'Issue2 execution evidence {report["RUN_ID"]}/{report["RUN_ATTEMPT"]}', 'content':base64.b64encode((json.dumps(report,ensure_ascii=False,indent=2)+'\n').encode()).decode()}
try:
    body['sha'] = json.loads(gh(f'repos/{repo}/contents/{path}?ref={branch}'))['sha']
except subprocess.CalledProcessError:
    pass
payload = Path(os.environ['RUNNER_TEMP'])/'issue2-evidence-request.json'
payload.write_text(json.dumps(body))
gh(f'repos/{repo}/contents/{path}', '--method', 'PUT', '--input', str(payload))
print(json.dumps(report, ensure_ascii=False, indent=2))
