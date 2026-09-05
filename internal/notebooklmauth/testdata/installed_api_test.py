"""Read-only installed-0.10.1 compatibility tests. All auth/CDP inputs are fake.

Run with the installed CLI interpreter. Network connects are prohibited, profile
storage is redirected to a fresh temporary directory before importing the CLI.
"""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

scratch = tempfile.TemporaryDirectory()
os.environ['NOTEBOOKLM_MCP_CLI_PATH'] = scratch.name
# Neither sockets nor browser/CLI child processes may escape this test.
def no_network(event, args):
    if event == 'socket.connect':
        raise AssertionError('test attempted a real network connection')
sys.addaudithook(no_network)

spec = importlib.util.spec_from_file_location('helper', Path(__file__).parent.parent / 'helper.py')
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)
helper.configure()
from notebooklm_tools.core import auth
from notebooklm_tools.core.base import BaseClient
from notebooklm_tools.core.client import NotebookLMClient
from notebooklm_tools.utils import cdp

HTML = '{"SNlM0e":"FAKE_CSRF","FdrFJe":"123","cfb2h":"FAKE_BUILD","oPEP7c":"fixture@example.test"}'
COOKIES = [{'name':'SID', 'value':'FAKE_ONLY', 'domain':'.google.com', 'path':'/'}]

class InstalledAPI(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.TemporaryDirectory(dir=scratch.name)
        directory = Path(self.dir.name)
        class Manager(auth.AuthManager):
            @property
            def profile_dir(self):
                return directory
        self.manager = Manager('fake-profile')
        self.manager.save_profile(cookies={'SID':'STALE_FAKE'}, email='fixture@example.test')
        self.before = {p.name:p.read_bytes() for p in directory.iterdir()}
        self.calls = []
        self.page_url = 'https://notebook.google.com/'
        self.html = HTML
        self.validated = False
        self.failure = False
        self.malformed = False

    def tearDown(self):
        self.dir.cleanup()

    def cdp_command(self, ws, method, params, **kwargs):
        self.assertEqual(ws, 'ws://127.0.0.1:1/devtools/page/fake')
        self.assertEqual(kwargs, {'retry':False, 'response_timeout':5})
        self.calls.append(method)
        if method == 'Runtime.evaluate':
            return {'result':{'value':json.dumps({'url':self.page_url, 'html':self.html})}}
        self.assertEqual(method, 'Network.getCookies')
        self.assertEqual(params, {'urls':['https://notebook.google.com/']})
        return {'cookies':COOKIES}

    def validate(self, client):
        self.assertEqual(client.cookies, COOKIES)
        self.assertEqual(client.csrf_token, 'FAKE_CSRF')
        self.assertFalse(client._try_reload_or_headless_auth())
        self.assertFalse(client._cdp_rpc_transport_enabled())
        client._update_cached_tokens()
        self.assertEqual(self.before, {p.name:p.read_bytes() for p in Path(self.dir.name).iterdir()})
        if self.failure:
            raise RuntimeError('FAKE_SECRET')
        if self.malformed:
            return None
        self.validated = True
        return []

    def restore(self):
        original_save = self.manager.save_profile
        owner = self
        class FakeWebSocket:
            def send(self, payload):
                request = json.loads(payload)
                result = owner.cdp_command('ws://127.0.0.1:1/devtools/page/fake',
                    request['method'], request['params'], retry=False, response_timeout=5)
                self.response = json.dumps({'id': request['id'], 'result': result})
            def recv(self): return self.response
            def settimeout(self, value): owner.assertEqual(value, 5)
            def close(self): pass
        def save(**kwargs):
            self.assertTrue(self.validated, 'save before validation')
            return original_save(**kwargs)
        with patch.object(auth, 'get_auth_manager', return_value=self.manager), \
             patch.object(cdp.websocket, 'create_connection', return_value=FakeWebSocket()), \
             patch.object(cdp, '_cached_ws', None), patch.object(cdp, '_cached_ws_url', None), \
             patch.object(BaseClient, '_call_rpc', lambda client, *args, **kwargs: self.validate(client)), \
             patch.object(self.manager, 'save_profile', side_effect=save):
            helper.restore({'type':'page', 'url':'https://notebook.google.com/',
                            'webSocketDebuggerUrl':'ws://127.0.0.1:1/devtools/page/fake'})

    def unchanged(self):
        self.assertEqual(self.before, {p.name:p.read_bytes() for p in Path(self.dir.name).iterdir()})

    def test_success_uses_installed_parsers_client_and_auth_manager(self):
        self.restore()
        self.assertTrue(self.validated)
        self.assertEqual(json.loads(self.manager.cookies_file.read_text()), COOKIES)
        self.assertEqual(self.calls, ['Runtime.evaluate','Network.getCookies','Runtime.evaluate'])

    def test_session_id_is_optional_when_candidate_rpc_validates(self):
        self.html = HTML.replace(',"FdrFJe":"123"', '')
        self.restore()
        self.assertTrue(self.validated)
        self.assertEqual(json.loads(self.manager.cookies_file.read_text()), COOKIES)

    def test_malformed_rpc_is_not_valid_auth(self):
        self.malformed = True
        with self.assertRaises(RuntimeError): self.restore()
        self.unchanged()

    def test_validation_failure_preserves_profile(self):
        self.failure = True
        with self.assertRaises(RuntimeError): self.restore()
        self.unchanged()

    def test_wrong_account_preserves_profile(self):
        self.html = HTML.replace('fixture@example.test', 'other@example.test')
        with self.assertRaises(RuntimeError): self.restore()
        self.assertFalse(self.validated)
        self.unchanged()

    def test_missing_identity_preserves_profile(self):
        self.html = HTML.replace('oPEP7c','untrusted-content-email')
        with self.assertRaises(RuntimeError): self.restore()
        self.unchanged()

    def test_page_at_login_wall_never_reads_cookies(self):
        self.page_url = 'https://accounts.google.com/?continue=https://notebook.google.com/'
        with self.assertRaises(RuntimeError): self.restore()
        self.assertEqual(self.calls,['Runtime.evaluate'])
        self.unchanged()

    def test_check_keeps_installed_auth_manager_writes_in_memory(self):
        from notebooklm_tools.cli import main
        original = auth.AuthManager.save_profile
        def check():
            self.assertEqual(sys.argv[1:], ['login', '--check'])
            self.manager.save_profile(cookies=COOKIES, email='fixture@example.test')
        with patch.object(main, 'cli_main', side_effect=check), patch.object(auth.AuthManager, 'save_profile', original):
            helper.cli(['login', '--check'])
        self.unchanged()

    def test_installed_host_recognition(self):
        for host in ['notebook.google.com', 'notebooklm.google.com', 'notebooklm.cloud.google.com', 'notebook.cloud.google.com', 'vertexaisearch.cloud.google.com']:
            self.assertTrue(helper.recognized('https://' + host + '/'))
        self.assertFalse(helper.recognized('https://accounts.google.com/?continue=https://notebook.google.com/'))

    def test_unattended_browser_launch_and_bare_login_impossible(self):
        with self.assertRaises(RuntimeError): helper.cli(['login'])
        with self.assertRaises(RuntimeError): subprocess.run(['/bin/true'])
        self.assertFalse(BaseClient._try_reload_or_headless_auth(None))
        self.assertFalse(BaseClient._cdp_rpc_transport_enabled(None))

if __name__ == '__main__':
    unittest.main(verbosity=2)
