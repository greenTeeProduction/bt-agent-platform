"""Unattended adapter for notebooklm-mcp-cli 0.10.1 (embedded in Go).

Only restore mode reads an existing page; CLI mode cannot use browser recovery.
No credential values or exception transcripts cross the subprocess boundary.
"""
import contextlib
import importlib.metadata
import io
import json
import logging
import re
import sys
from urllib.parse import urlparse


def no_processes(event, args):
    # Includes indirect webbrowser/managed-browser launches by CLI dependencies.
    if event in {"subprocess.Popen", "os.system", "os.exec", "os.posix_spawn", "os.fork", "os.forkpty"}:
        raise RuntimeError("process creation disabled in unattended NotebookLM")


def configure():
    if importlib.metadata.version("notebooklm-mcp-cli") != "0.10.1":
        raise RuntimeError("unsupported CLI API")
    sys.addaudithook(no_processes)
    logging.disable(logging.CRITICAL)
    from notebooklm_tools.core.base import BaseClient

    # The installed client can launch headless Chrome or create a CDP page on
    # ordinary RPC failure. Only the Go policy may initiate session restoration.
    BaseClient._try_reload_or_headless_auth = lambda self: False
    BaseClient._update_cached_tokens = lambda self: None
    BaseClient._cdp_rpc_transport_enabled = lambda self: False


def recognized(url):
    from notebooklm_tools.utils.cdp import _is_notebooklm_url

    parsed = urlparse(url)
    return parsed.scheme == "https" and not parsed.username and _is_notebooklm_url(url)


def restore(target):
    from notebooklm_tools.core.auth import get_auth_manager
    from notebooklm_tools.core.client import NotebookLMClient
    from notebooklm_tools.utils import cdp

    ws = target["webSocketDebuggerUrl"]
    if target["type"] != "page" or not recognized(target["url"]):
        raise RuntimeError("unrecognized target")

    def command(method, params):
        return cdp.execute_cdp_command(ws, method, params, retry=False, response_timeout=5)

    def snapshot():
        value = command("Runtime.evaluate", {
            "expression": "JSON.stringify({url:location.href,html:document.documentElement.outerHTML})",
            "returnByValue": True,
        })["result"]["value"]
        page = json.loads(value)
        if not recognized(page["url"]):
            raise RuntimeError("page left NotebookLM")
        return page

    page = snapshot()
    # Limit cookies to the recognized page's origin, not all browser cookies.
    origin = "https://" + urlparse(page["url"]).hostname
    cookies = command("Network.getCookies", {"urls": [origin + "/"]})["cookies"]
    after = snapshot()
    html = page["html"]
    # Use the authenticated account field, never an arbitrary email in content.
    match = re.search(r'"oPEP7c":"([^"\s]+@[^"\s]+)"', html)
    email = match.group(1) if match else ""
    csrf = cdp.extract_csrf_token(html)
    session = cdp.extract_session_id(html)
    build = cdp.extract_build_label(html)
    # Current NotebookLM pages can omit FdrFJe; the installed client treats
    # session_id as optional. The authenticated candidate RPC is the authority.
    if not cookies or not csrf or not email:
        raise RuntimeError("incomplete session")
    after_email = re.search(r'"oPEP7c":"([^"\s]+@[^"\s]+)"', after["html"])
    if (after["url"] != page["url"] or not after_email or after_email.group(1) != email
            or cdp.extract_csrf_token(after["html"]) != csrf
            or cdp.extract_session_id(after["html"]) != session):
        raise RuntimeError("page session changed during extraction")

    manager = get_auth_manager()  # Same configured default/profile store as nlm.
    if manager.profile_exists():
        saved = manager.load_profile()
        if not saved.email or saved.email != email:
            raise RuntimeError("cannot confirm profile account")

    # Candidate validation must not reload disk credentials, write a token cache,
    # start headless auth, or silently switch accounts by refreshing the page.
    class CandidateClient(NotebookLMClient):
        def _refresh_auth_tokens(self):
            raise RuntimeError("candidate credentials rejected")

        def _call_rpc(self, *args, **kwargs):
            result = super()._call_rpc(*args, **kwargs)
            # list_notebooks otherwise turns malformed/null RPC data into [],
            # which must not count as positive authentication evidence.
            if not isinstance(result, list):
                raise RuntimeError("invalid candidate RPC response")
            return result

    with CandidateClient(cookies=cookies, csrf_token=csrf, session_id=session,
                         build_label=build, base_host=urlparse(page["url"]).hostname,
                         profile_name=manager.profile_name) as client:
        notebooks = client.list_notebooks()
        if not isinstance(notebooks, list):
            raise RuntimeError("invalid validation response")

    # AccountMismatchError remains enforced; never use force or delete profiles.
    manager.save_profile(cookies=cookies, csrf_token=csrf, session_id=session,
                         email=email, build_label=build,
                         base_host=urlparse(page["url"]).hostname, force=False)


def cli(args):
    if not args or (args[0] == "login" and args != ["login", "--check"]):
        raise RuntimeError("interactive login disabled")
    from notebooklm_tools.core.auth import AuthManager, Profile
    from notebooklm_tools.cli.main import cli_main

    # check_auth refreshes metadata even for a read-only check. Keep its updates
    # in memory so failed checks and network outages preserve all profile files.
    def memory_save(self, cookies, **kwargs):
        self._profile = Profile(name=self.profile_name, cookies=cookies,
                                **{k: v for k, v in kwargs.items()
                                   if k in {"csrf_token", "session_id", "email", "build_label", "base_host"}})
        return self._profile

    AuthManager.save_profile = memory_save
    sys.argv = ["nlm", *args]
    cli_main()


def main():
    mode = sys.argv[1]
    try:
        configure()
        if mode == "restore":
            # Suppress dependency output (including exception text with tokens).
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                restore(json.load(sys.stdin))
            print("restored")
        elif mode == "cli":
            cli(sys.argv[2:])
        else:
            raise RuntimeError("invalid mode")
    except Exception:
        print("auth_required: existing session restoration failed; saved credentials preserved"
              if mode == "restore" else "auth_error: unattended CLI failed", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
