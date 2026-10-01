package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

var ErrRestartDeferred = errors.New("target owner deferred restart")

const restartControlTimeout = 5 * time.Second

// RestartControlConfig belongs to the target process, not the fleet controller.
// BeginRestart must atomically seal all owned execution admission when idle.
// Missing owners/unsupported platforms never permit a direct sibling restart.
type RestartControlConfig struct {
	Home, Unit, Revision, BinaryPath string
	Enabled                          bool
	BeginRestart                     func() (func(bool), bool)
	// Optional implementation seams for controlled fixtures. Production uses
	// the configured artifact's --version and the bounded systemd command.
	VerifyArtifact func(path, unit, revision string) error
	VerifyOwner    func(unit string) error
	Restart        func(unit string) error
}

type restartControlRequest struct {
	Revision string `json:"revision"`
}

type restartControlResponse struct {
	Unit     string `json:"unit"`
	Revision string `json:"revision"`
	Status   string `json:"status"`
}

func restartControlAddress(home, unit string) (*net.UnixAddr, error) {
	switch unit {
	case "bt-agent", "bt-dashboard", "bt-gardener":
	default:
		return nil, fmt.Errorf("unsupported restart unit")
	}
	if home == "" {
		return nil, fmt.Errorf("restart owner home is required")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return nil, err
	}
	if resolvedHome, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolvedHome
	}
	sum := sha256.Sum256([]byte(abs))
	return platformRestartAddress(hex.EncodeToString(sum[:16]), unit)
}

func validRestartRevision(revision string) bool {
	if len(revision) != 40 && len(revision) != 64 {
		return false
	}
	_, err := hex.DecodeString(revision)
	return err == nil && revision == strings.ToLower(revision)
}

// StartRestartControl serves only same-UID peers. Linux abstract Unix sockets
// avoid stale socket files and pathname-length limits; both ends verify kernel
// credentials. The namespace is scoped to the configured platform home/unit.
func StartRestartControl(cfg RestartControlConfig) (stop func(), err error) {
	if cfg.BeginRestart == nil || cfg.BinaryPath == "" {
		return nil, fmt.Errorf("restart owner admission/artifact configuration is required")
	}
	addr, err := restartControlAddress(cfg.Home, cfg.Unit)
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, err
	}
	if cfg.VerifyArtifact == nil {
		cfg.VerifyArtifact = verifyRestartArtifact
	}
	if cfg.Restart == nil {
		cfg.Restart = defaultDriftRestart
	}
	if cfg.VerifyOwner == nil {
		cfg.VerifyOwner = verifyRestartUnitOwner
	}
	done := make(chan struct{})
	var handlers sync.WaitGroup
	slots := make(chan struct{}, 4)
	reliability.SafeGo("restart-control", func() {
		defer close(done)
		for {
			conn, acceptErr := listener.AcceptUnix()
			if acceptErr != nil {
				return
			}
			if peerErr := verifyRestartPeer(conn); peerErr != nil {
				_ = conn.Close()
				continue
			}
			select {
			case slots <- struct{}{}:
			default:
				_ = conn.Close()
				continue
			}
			handlers.Add(1)
			reliability.SafeGoWithCleanup("restart-control-request", func() {
				serveRestartControl(conn, cfg)
			}, nil, func() { _ = conn.Close(); <-slots; handlers.Done() })
		}
	}, nil)
	return sync.OnceFunc(func() { _ = listener.Close(); <-done; handlers.Wait() }), nil
}

func serveRestartControl(conn *net.UnixConn, cfg RestartControlConfig) {
	_ = conn.SetDeadline(time.Now().Add(restartControlTimeout))
	limited := &io.LimitedReader{R: conn, N: 1025}
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	var req restartControlRequest
	response := restartControlResponse{Unit: cfg.Unit, Status: "rejected"}
	defer func() { _ = json.NewEncoder(conn).Encode(response) }()
	if err := decoder.Decode(&req); err != nil || !validRestartRevision(req.Revision) {
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || limited.N <= 0 {
		return
	}
	response.Revision = req.Revision
	if !cfg.Enabled {
		response.Status = "disabled"
		return
	}
	if err := cfg.VerifyOwner(cfg.Unit); err != nil {
		response.Status = "owner-rejected"
		return
	}
	if cfg.Revision == req.Revision {
		response.Status = "current"
		return
	}
	finish, ready := cfg.BeginRestart()
	if !ready {
		response.Status = "busy"
		return
	}
	if finish == nil {
		return
	}
	// Unexpected failure after admission may hide an accepted systemd request.
	// Preserve the seal unless we establish a rejection before accepting it.
	keepSealed := true
	response.Status = "uncertain"
	defer func() { finish(keepSealed) }()
	if err := cfg.VerifyArtifact(cfg.BinaryPath, cfg.Unit, req.Revision); err != nil {
		keepSealed = false
		response.Status = "artifact-rejected"
		return
	}
	if err := cfg.Restart(cfg.Unit); err != nil {
		keepSealed = reliability.IsExecutionUncertainError(err)
		response.Status = "rejected"
		if keepSealed {
			response.Status = "uncertain"
		}
		return
	}
	response.Status = "accepted"
}

// RequestOwnedRestart never invokes systemd for another process. A lost reply
// after sending the request is uncertainty, not permission to bypass its owner.
func RequestOwnedRestart(home, unit, revision string) error {
	if !validRestartRevision(revision) {
		return fmt.Errorf("invalid restart revision")
	}
	addr, err := restartControlAddress(home, unit)
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: restartControlTimeout}
	connection, err := dialer.Dial("unix", addr.Name)
	if err != nil {
		return fmt.Errorf("restart owner unavailable: %w", err)
	}
	conn, ok := connection.(*net.UnixConn)
	if !ok {
		_ = connection.Close()
		return fmt.Errorf("restart owner transport differs")
	}
	defer func() { _ = conn.Close() }()
	if err := verifyRestartPeer(conn); err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(restartControlTimeout))
	uncertain := func(err error) error { return &reliability.ExecutionUncertainError{Err: err} }
	if err := json.NewEncoder(conn).Encode(restartControlRequest{Revision: revision}); err != nil {
		return uncertain(err)
	}
	if err := conn.CloseWrite(); err != nil {
		return uncertain(err)
	}
	var response restartControlResponse
	if err := json.NewDecoder(io.LimitReader(conn, 1024)).Decode(&response); err != nil {
		return uncertain(err)
	}
	if response.Unit != unit || response.Revision != revision {
		return uncertain(fmt.Errorf("restart owner returned mismatched identity"))
	}
	switch response.Status {
	case "accepted", "current":
		return nil
	case "busy", "disabled", "rejected", "artifact-rejected", "owner-rejected":
		return ErrRestartDeferred
	default:
		return uncertain(fmt.Errorf("restart disposition is unknown"))
	}
}

// A control namespace or matching UID does not establish systemd ownership.
// Verify the configured unit's live MainPID before attesting or requesting its
// restart. This read-only failure proves no restart has yet been dispatched.
func verifyRestartUnitOwner(unit string) error {
	switch unit {
	case "bt-agent", "bt-dashboard", "bt-gardener":
	default:
		return fmt.Errorf("unsupported restart unit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), restartControlTimeout)
	defer cancel()
	// #nosec G204 -- unit is restricted above to the three canonical daemon
	// names; the executable/flags are fixed and no shell or request revision is
	// used. This read-only query attests the target's systemd MainPID.
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "show", "--property=MainPID", "--value", unit+".service")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("restart unit identity unavailable: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 || pid != os.Getpid() {
		return fmt.Errorf("restart unit MainPID differs from this owner")
	}
	return nil
}

func verifyRestartArtifact(path, unit, revision string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// #nosec G204 -- path is the owning daemon's configured executable, never
	// supplied by the request. Only its fixed --version invocation is allowed.
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("restart artifact version probe failed: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || fields[0] != unit {
		return fmt.Errorf("restart artifact unit identity differs")
	}
	values := map[string]string{}
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return fmt.Errorf("restart artifact version metadata is malformed")
		}
		if _, duplicate := values[key]; duplicate {
			return fmt.Errorf("restart artifact version metadata is ambiguous")
		}
		values[key] = value
	}
	if values["revision"] != revision || values["dirty"] != "false" {
		return fmt.Errorf("restart artifact revision or clean state differs")
	}
	return nil
}
