"""Unattended adapter for notebooklm-mcp-cli 0.14.x (embedded in Go).

Only restore mode reads an existing page; CLI mode cannot use browser recovery.
No credential values or exception transcripts cross the subprocess boundary.
"""
import contextlib
import importlib.metadata
import io
import json
import logging
import os
import re
import sys
from urllib.parse import urlparse


def no_processes(event, args):
    # Includes indirect webbrowser/managed-browser launches by CLI dependencies.
    if event in {"subprocess.Popen", "os.system", "os.exec", "os.posix_spawn", "os.fork", "os.forkpty"}:
        raise RuntimeError("process creation disabled in unattended NotebookLM")


class UnsupportedCLIError(RuntimeError):
    pass


def configure():
    version = importlib.metadata.version("notebooklm-mcp-cli")
    if not re.fullmatch(r"0\.14\.\d+", version):
        raise UnsupportedCLIError("NotebookLM adapter requires notebooklm-mcp-cli 0.14.x; update the adapter after a minor-version upgrade")
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


def memory_auth_writes():
    from notebooklm_tools.core.auth import AuthManager, Profile

    # check_auth refreshes metadata even for a read-only check. Keep its updates
    # in memory so failed checks and network outages preserve all profile files.
    def memory_save(self, cookies, **kwargs):
        self._profile = Profile(name=self.profile_name, cookies=cookies,
                                **{k: v for k, v in kwargs.items()
                                   if k in {"csrf_token", "session_id", "email", "build_label", "base_host"}})
        return self._profile

    AuthManager.save_profile = memory_save
    # 0.14 also has a metadata-only persistence path used by health checks.
    AuthManager.update_metadata = lambda self, **kwargs: None


def save_renewed_profile(manager, saved, **fields):
    """Use 0.14's bounded profile lock and atomic, storage-mode-aware writer.

    Compare the account and credentials under the upstream lock so an external
    login/migration during candidate validation is never silently overwritten.
    """
    from notebooklm_tools.core.credential_store import get_profile_lock

    with get_profile_lock(manager.profile_name):
        current = manager.load_profile(force_reload=True)
        if (current.email != saved.email or current.cookies != saved.cookies
                or current.revision != saved.revision
                or current.csrf_token != saved.csrf_token):
            raise RuntimeError("profile changed while renewing")
        manager.save_profile(force=False, expected_revision=saved.revision, **fields)


def renew():
    """Rotate SIDTS in memory, verify account + RPC, then save the same profile.

    RotateCookies follows notebooklm-py's documented keepalive protocol. It is
    session renewal, not an API key or a guarantee against Google revocation.
    """
    import httpx
    from notebooklm_tools.core import auth
    from notebooklm_tools.core.base import BaseClient
    from notebooklm_tools.core.client import NotebookLMClient
    from notebooklm_tools.utils import cdp

    def verdict(status, detail):
        return {"status": status, "detail": detail}

    try:
        manager = auth.get_auth_manager()
        if not manager.profile_exists():
            return verdict("auth_required", "no saved profile; operator login required")
        saved = manager.load_profile()
        if not saved.email:
            return verdict("auth_required", "saved account identity missing; operator login required")
        host = saved.base_host or "notebooklm.google.com"
        if not recognized("https://" + host + "/"):
            return verdict("auth_error", "saved NotebookLM host is unsupported")
        jar = BaseClient._get_httpx_cookies(saved)
        with httpx.Client(cookies=jar, timeout=12, follow_redirects=True,
                          headers=auth._PAGE_FETCH_HEADERS) as http:
            rotation = http.post("https://accounts.google.com/RotateCookies",
                                 headers={"Content-Type": "application/json",
                                          "Origin": "https://accounts.google.com"},
                                 content='[000,"-0000000000000000000"]')
            rotation.raise_for_status()
            page = http.get("https://" + host + "/")
            page.raise_for_status()
            identity = re.search(r'"oPEP7c":"([^"\s]+@[^"\s]+)"', page.text)
            csrf = cdp.extract_csrf_token(page.text)
            if not recognized(str(page.url)) or not identity or not csrf:
                return verdict("auth_required", "session renewal requires an authenticated Google session")
            if identity.group(1) != saved.email:
                return verdict("auth_error", "renewal account mismatch; saved profile preserved")
            cookies = [{"name": c.name, "value": c.value, "domain": c.domain,
                        "path": c.path, "secure": c.secure, "expires": c.expires}
                       for c in http.cookies.jar if c.domain.lstrip(".") == "google.com"
                       or c.domain.endswith(".google.com")]
        session, build = cdp.extract_session_id(page.text), cdp.extract_build_label(page.text)

        class CandidateClient(NotebookLMClient):
            def _refresh_auth_tokens(self):
                raise RuntimeError("candidate renewal rejected")

            def _call_rpc(self, *args, **kwargs):
                response = super()._call_rpc(*args, **kwargs)
                if not isinstance(response, list):
                    raise RuntimeError("invalid renewal RPC response")
                return response

        with CandidateClient(cookies=cookies, csrf_token=csrf, session_id=session,
                             build_label=build, base_host=urlparse(str(page.url)).hostname,
                             profile_name=manager.profile_name) as client:
            if not isinstance(client.list_notebooks(), list):
                raise RuntimeError("invalid renewal response")
        save_renewed_profile(manager, saved, cookies=cookies, csrf_token=csrf,
                            session_id=session, email=saved.email, build_label=build,
                            base_host=urlparse(str(page.url)).hostname)
        return verdict("valid", "session renewed and verified against the saved account")
    except httpx.HTTPStatusError as error:
        status = error.response.status_code
        if status in {401, 403}:
            return verdict("auth_required", "Google rejected session renewal; operator login required")
        if status == 429 or status >= 500:
            return verdict("network_error", f"Google renewal HTTP {status}; saved profile preserved")
        return verdict("auth_error", f"Google renewal HTTP {status}; saved profile preserved")
    except httpx.HTTPError:
        return verdict("network_error", "session renewal could not reach Google; saved profile preserved")
    except Exception:
        return verdict("auth_error", "session renewal failed validation or persistence; operator check required")


def cli(args):
    if not args or (args[0] == "login" and args != ["login", "--check"]):
        raise RuntimeError("interactive login disabled")
    from notebooklm_tools.cli.main import cli_main
    memory_auth_writes()
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
        elif mode == "renew":
            with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                result = renew()
            print(json.dumps(result))
            if result["status"] != "valid":
                sys.exit(1)
        elif mode == "mcp":
            memory_auth_writes()
            disabled = {name.strip() for name in os.environ.get("NOTEBOOKLM_DISABLED_TOOLS", "").split(",")}
            disabled.update({"refresh_auth", "save_auth_tokens"})
            os.environ["NOTEBOOKLM_DISABLED_TOOLS"] = ",".join(sorted(disabled - {""}))
            # Explicit enables must not undo the shared auth policy.
            enabled = {name.strip() for name in os.environ.get("NOTEBOOKLM_ENABLED_TOOLS", "").split(",")}
            os.environ["NOTEBOOKLM_ENABLED_TOOLS"] = ",".join(sorted(enabled - disabled - {""}))
            from notebooklm_tools.mcp.server import main as mcp_main
            sys.argv = ["notebooklm-mcp", "--transport", "stdio"]
            mcp_main()
        else:
            raise RuntimeError("invalid mode")
    except UnsupportedCLIError as error:
        print("auth_error: " + str(error), file=sys.stderr)
        sys.exit(1)
    except Exception:
        print("auth_required: existing session restoration failed; saved credentials preserved"
              if mode == "restore" else "auth_error: unattended CLI failed", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
