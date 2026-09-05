"""Subprocess fixture: only synthetic credentials and temporary files.

Implements the installed CLI/CDP API boundary; executes the actual embedded
helper, so success depends on extracting, validating, saving and rechecking.
"""
import importlib.metadata
import json
import os
from pathlib import Path
import sys
import time
import types

root = Path(__file__).parent
config = json.loads((root / 'fixture.json').read_text())

def event(name):
    with (root / 'events').open('a') as f:
        f.write(name + '\n')

def module(name):
    m = types.ModuleType(name)
    sys.modules[name] = m
    if '.' in name:
        parent, child = name.rsplit('.', 1)
        setattr(sys.modules[parent], child, m)
    return m

for name in ['notebooklm_tools', 'notebooklm_tools.core', 'notebooklm_tools.utils',
             'notebooklm_tools.cli', 'notebooklm_tools.core.base',
             'notebooklm_tools.core.auth', 'notebooklm_tools.core.client',
             'notebooklm_tools.utils.cdp', 'notebooklm_tools.cli.main']:
    module(name)
importlib.metadata.version = lambda name: '0.10.1'

class BaseClient:
    pass
sys.modules['notebooklm_tools.core.base'].BaseClient = BaseClient

class Profile:
    def __init__(self, **kwargs):
        self.__dict__.update(kwargs)

class Manager:
    profile_name = 'configured-default'
    def profile_exists(self):
        return True
    def load_profile(self):
        return Profile(email=config.get('saved_email', 'fixture@example.test'))
    def save_profile(self, **kwargs):
        assert kwargs['force'] is False
        assert (root / 'validated').exists(), 'write before candidate validation'
        assert kwargs['email'] == self.load_profile().email
        assert kwargs['cookies'] == [{'name': 'SID', 'value': 'FAKE_ONLY'}]
        event('save')
        (root / 'saved').write_text('valid')

sys.modules['notebooklm_tools.core.auth'].get_auth_manager = Manager
sys.modules['notebooklm_tools.core.auth'].AuthManager = Manager
sys.modules['notebooklm_tools.core.auth'].Profile = Profile

class Client(BaseClient):
    def __init__(self, **kwargs):
        assert kwargs['profile_name'] == 'configured-default'
        assert self._try_reload_or_headless_auth() is False
        assert self._cdp_rpc_transport_enabled() is False
        self._update_cached_tokens()
    def __enter__(self):
        return self
    def __exit__(self, *args):
        pass
    def list_notebooks(self):
        event('validate')
        if config.get('validation_failure'):
            raise RuntimeError('FAKE_SECRET must never reach output')
        (root / 'validated').write_text('yes')
        return []  # A valid account can have zero notebooks.

sys.modules['notebooklm_tools.core.client'].NotebookLMClient = Client
cdp = sys.modules['notebooklm_tools.utils.cdp']
from urllib.parse import urlparse
cdp._is_notebooklm_url = lambda url: urlparse(url).hostname in {
 'notebook.google.com', 'notebooklm.google.com', 'notebooklm.cloud.google.com',
 'notebook.cloud.google.com', 'vertexaisearch.cloud.google.com'}
html = '{"SNlM0e":"FAKE_CSRF","FdrFJe":"123","cfb2h":"FAKE_BUILD","oPEP7c":"fixture@example.test"}'
cdp.extract_csrf_token = lambda s: json.loads(s)['SNlM0e']
cdp.extract_session_id = lambda s: json.loads(s)['FdrFJe']
cdp.extract_build_label = lambda s: json.loads(s)['cfb2h']

def command(ws, method, params, **kwargs):
    assert ws.endswith('/devtools/page/existing')
    assert kwargs == {'retry': False, 'response_timeout': 5}
    event(method)
    if config.get('closed_target'):
        raise RuntimeError('target closed')
    if config.get('hang'):
        (root / 'pid').write_text(str(os.getpid()))
        time.sleep(60)
    if method == 'Runtime.evaluate':
        assert params['expression'] == 'JSON.stringify({url:location.href,html:document.documentElement.outerHTML})'
        return {'result': {'value': json.dumps({'url': config.get('current_url', config['page_url']), 'html': html})}}
    if method == 'Network.getCookies':
        assert params['urls'] == ['https://' + urlparse(config['page_url']).hostname + '/']
        return {'cookies': [{'name': 'SID', 'value': 'FAKE_ONLY'}]}
    raise AssertionError('forbidden CDP method ' + method)
cdp.execute_cdp_command = command

def cli_main():
    assert sys.argv[1:] == ['login', '--check'], 'GUI login must never be invoked'
    event('check')
    if config.get('check_output'):
        print(config['check_output'])
    elif (root / 'saved').read_text() == 'valid' and not config.get('recheck_failure'):
        print('Authentication valid!')
    else:
        print('Authentication failed: Credentials have expired.')
        sys.exit(1)
sys.modules['notebooklm_tools.cli.main'].cli_main = cli_main

# Interpreter wrapper passes -B -c <actual embedded source> <mode> ...
source = sys.argv[3]
sys.argv = ['helper', *sys.argv[4:]]
exec(compile(source, 'embedded-helper.py', 'exec'), {'__name__': '__main__'})
