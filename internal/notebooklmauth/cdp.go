package notebooklmauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

type cdpPage struct {
	Type      string `json:"type"`
	URL       string `json:"url"`
	WebSocket string `json:"webSocketDebuggerUrl"`
}

// existingBrowserRestore selects an existing target and never creates, navigates,
// closes or launches browser pages. A closed/changed target fails safely.
func existingBrowserRestore(ctx context.Context, endpoint string) Result {
	return restoreWithCommand(ctx, endpoint, helperCommand)
}

func restoreWithCommand(ctx context.Context, endpoint string, command func(context.Context, string, ...string) *exec.Cmd) Result {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Result{Status: "auth_required", Detail: "invalid BT_NOTEBOOKLM_CDP_URL; provide the existing browser's HTTP CDP endpoint"}
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/json/list"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{Status: "auth_required", Detail: "cannot inspect existing browser"}
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		// CDP is an explicit endpoint, never route it through ambient proxies.
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return Result{Status: "auth_required", Detail: "existing CDP browser unavailable; operator session restore required"}
	}
	defer resp.Body.Close()
	var pages []cdpPage
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&pages) != nil {
		return Result{Status: "auth_required", Detail: "cannot inspect existing CDP browser; operator session restore required"}
	}
	for _, page := range pages {
		pageURL, err := url.Parse(page.URL)
		if err != nil || page.Type != "page" || pageURL.Scheme != "https" || pageURL.User != nil {
			continue
		}
		switch strings.ToLower(pageURL.Hostname()) {
		case "notebook.google.com", "notebooklm.google.com", "notebooklm.cloud.google.com", "notebook.cloud.google.com", "vertexaisearch.cloud.google.com":
		default:
			continue
		}
		ws, err := url.Parse(page.WebSocket)
		// A debugger URL must point back at the configured CDP server, not an
		// arbitrary endpoint advertised by a target. Accept loopback aliases.
		if err != nil || (ws.Scheme != "ws" && ws.Scheme != "wss") || ws.User != nil ||
			!sameCDPHost(ws.Hostname(), u.Hostname()) || ws.Port() != u.Port() || !strings.HasPrefix(ws.Path, "/devtools/page/") {
			continue
		}
		payload, err := json.Marshal(page)
		if err != nil {
			return Result{Status: "auth_error", Detail: "cannot encode existing target"}
		}
		cmd := command(ctx, "restore")
		cmd.Stdin = bytes.NewReader(payload)
		out, err := cmd.Output() // Never expose dependency stderr or credential data.
		if err == nil && strings.TrimSpace(string(out)) == "restored" {
			return Result{Status: "valid", Detail: "existing browser credentials validated and saved; saved-auth recheck required"}
		}
		return Result{Status: "auth_required", Detail: "existing session restoration failed; operator session restore required"}
	}
	return Result{Status: "auth_required", Detail: "no existing NotebookLM page (or browser is at a login wall); operator session restore required"}
}

func sameCDPHost(a, b string) bool {
	loopback := func(s string) bool { return s == "localhost" || s == "127.0.0.1" || s == "::1" }
	return strings.EqualFold(a, b) || (loopback(a) && loopback(b))
}
