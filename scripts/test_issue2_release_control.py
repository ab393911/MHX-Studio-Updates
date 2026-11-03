"""Release safety gates only; no network, release creation or application execution."""
import base64
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('release_control',ROOT/'scripts/issue2-release-control.py')
c=importlib.util.module_from_spec(spec);spec.loader.exec_module(c)
FROZEN=json.loads((ROOT/'docs/DIAGNOSTIC_VERIFICATION_20261103.json').read_text())

class ReleaseGates(unittest.TestCase):
    def test_frozen_native_and_installed_baseline_reports(self):
        c.validate_rollback(FROZEN['native_update'])
        c.validate_upgrade(FROZEN['native_update']['preview41_issue2_gui_upgrade'],c.PRIOR_VERSION)
        for key in c.GUI_KEYS:self.assertIs(FROZEN['windows_gui'][key],True)

    def test_wrong_source_version_or_lost_history_blocks_upgrade(self):
        for key,value in [('source_commit','wrong'),('version',c.PRIOR_VERSION),('role_execution_history_preserved',False),('studio_database_preserved',False),('confirmation_clicks',1)]:
            with self.subTest(key=key):
                d=copy.deepcopy(FROZEN['native_update']['preview41_issue2_gui_upgrade']);d[key]=value
                with self.assertRaises(RuntimeError):c.validate_upgrade(d,c.PRIOR_VERSION)

    def test_failed_rollback_or_false_unattended_claim_is_rejected(self):
        for scenario,key in [('startup_timeout','studio_order_database_preserved'),('config_migration_exit','config_unchanged'),('sha_tamper','passed')]:
            n=copy.deepcopy(FROZEN['native_update']);n[scenario][key]=False
            with self.assertRaises(RuntimeError):c.validate_rollback(n)
        n=copy.deepcopy(FROZEN['native_update']);n['interrupted_recovery']['automatic_without_restarting_helper']=True
        with self.assertRaises(RuntimeError):c.validate_rollback(n)

    def test_altered_artifact_and_mixed_metadata_are_rejected(self):
        # Hash gate is tested with inert bytes; never substitute a test artifact for the frozen EXE.
        body=b'\x00'*8514048;digest=hashlib.sha256(body).hexdigest()
        meta=copy.deepcopy(FROZEN['candidate']);meta['sha256']=digest
        with patch.object(c,'SHA',digest):
            c.validate_bytes(meta,body)
            with self.assertRaises(RuntimeError):c.validate_bytes(meta,body[:-1]+b'x')
            for key,value in [('source_commit','wrong'),('version','9.0.0'),('isolated_test_id','isolated')]:
                wrong=dict(meta);wrong[key]=value
                with self.assertRaises(RuntimeError):c.validate_bytes(wrong,body)

    def test_online_verification_failure_is_not_retried(self):
        with patch.object(c,'call',side_effect=subprocess.CalledProcessError(1,['verify'])) as call:
            with self.assertRaises(subprocess.CalledProcessError):c.verify_online(c.ASSET,'report.json')
            self.assertEqual(call.call_count,1)

    def context(self):
        manifest={'source_commit':c.SOURCE,'version':c.VERSION,'sha256':c.SHA}
        prior_text=json.dumps({'version':c.PRIOR_VERSION,'sha256':c.PRIOR_SHA})
        prior={'sha':'oldBlob','content':base64.b64encode(prior_text.encode()).decode()}
        recovery={key:True for key in ['passed','old_exe_restored','old_backup_retained','config_bytes_preserved','sqlite_order_preserved','restored_gui_visible','no_cmd_bat_python_or_manual_exe_copy']}
        recovery.update(baseline_version=c.PRIOR_VERSION,baseline_sha256=c.PRIOR_SHA)
        files={'dist/update.json':manifest,'reports/published-gui-update.json':copy.deepcopy(FROZEN['native_update']['preview41_issue2_gui_upgrade']),'reports/manual-recovery.json':recovery,'reports/public-rollback.json':FROZEN['native_update'],'dist/public-download.json':{'sha256':c.SHA},'dist/channel-before.json':{'stable_blob':c.STABLE_BLOB,'test_blob':prior['sha']}}
        return files,prior

    def test_failed_public_gate_never_writes_a_channel(self):
        files,_=self.context();files['reports/published-gui-update.json']['game_settings_byte_preserved']=False
        with patch.object(c,'load',side_effect=lambda p:files[p]),patch.object(c,'api') as api:
            with self.assertRaises(RuntimeError):c.promote()
            api.assert_not_called()

    def run_promotion(self,verification_fails=False,concurrent_change=False):
        files,prior=self.context();current=dict(prior);writes=[];saved={}
        if concurrent_change:current['sha']='someoneElsesBlob'
        endpoint=f'repos/{c.REPO}/contents/issue2-test.json'
        def api(path,body=None):
            if body is not None:
                self.assertEqual(path,endpoint) # A stable-manifest write must never occur.
                writes.append(copy.deepcopy(body));current['sha']='newBlob' if len(writes)==1 else 'restoredBlob'
                return {'content':{'sha':current['sha']},'commit':{'sha':'channelCommit'}}
            if '/releases/tags/' in path:return {'draft':False,'prerelease':True,'target_commitish':c.SOURCE}
            if '/preview.json?' in path:return {'sha':c.STABLE_BLOB}
            self.assertEqual(path,endpoint+'?ref=updates');return dict(current)
        with tempfile.TemporaryDirectory() as td:
            old=os.getcwd();os.chdir(td)
            try:
                Path('dist').mkdir();Path('dist/update.json').write_text(json.dumps(files['dist/update.json']))
                with patch.object(c,'load',side_effect=lambda p:files[p]),patch.object(c,'api',side_effect=api),patch.object(c,'save',side_effect=lambda p,v:saved.update({p:v})),patch.object(c,'call',return_value=''),patch.object(c,'verify_online',side_effect=RuntimeError('integrity mismatch') if verification_fails else None),patch.dict(os.environ,{'GITHUB_SHA':'releaseWorkflowCommit','GITHUB_RUN_ID':'unit-test'}):
                    if verification_fails or concurrent_change:
                        with self.assertRaises(RuntimeError):c.promote()
                    else:c.promote()
            finally:os.chdir(old)
        return writes,saved,prior

    def test_integrity_failure_restores_previous_test_channel(self):
        writes,_,prior=self.run_promotion(verification_fails=True)
        self.assertEqual(len(writes),2)
        self.assertEqual(writes[1]['sha'],'newBlob')
        self.assertEqual(writes[1]['content'],prior['content'])

    def test_concurrent_channel_change_is_not_overwritten(self):
        writes,_,_=self.run_promotion(concurrent_change=True);self.assertEqual(writes,[])

    def test_success_only_updates_test_manifest_and_records_commit(self):
        writes,saved,_=self.run_promotion()
        self.assertEqual(len(writes),1)
        self.assertEqual(saved['reports/issue2-publication.json']['channel_commit'],'channelCommit')
        self.assertTrue(saved['reports/issue2-publication.json']['default_preview_manifest_unchanged'])

if __name__=='__main__':unittest.main()
