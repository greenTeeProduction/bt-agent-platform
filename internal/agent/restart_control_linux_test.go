package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/reliability"
)

const controlRevision = "1234567890123456789012345678901234567890"

func TestRestartControlDispositionAndIdentity(t *testing.T) {
	for _, fixture := range []string{"busy", "disabled", "current", "wrong-owner", "artifact", "rejected", "uncertain", "accepted"} {
		t.Run(fixture, func(t *testing.T) {
			var sealed atomic.Bool
			var calls, probes atomic.Int64
			cfg := RestartControlConfig{Home: t.TempDir(), Unit: "bt-dashboard", Revision: strings.Repeat("a", 40), BinaryPath: "fixture-bin", Enabled: fixture != "disabled", VerifyOwner: func(string) error { return nil }}
			if fixture == "current" {
				cfg.Revision = controlRevision
			}
			if fixture == "wrong-owner" {
				cfg.Revision = controlRevision
				cfg.VerifyOwner = func(string) error { return errors.New("different systemd MainPID") }
			}
			cfg.BeginRestart = func() (func(bool), bool) {
				if fixture == "busy" || !sealed.CompareAndSwap(false, true) {
					return nil, false
				}
				return func(keep bool) { sealed.Store(keep) }, true
			}
			cfg.VerifyArtifact = func(_, unit, rev string) error {
				probes.Add(1)
				if unit != cfg.Unit || rev != controlRevision || !sealed.Load() {
					return errors.New("unsealed or mismatched verification")
				}
				if fixture == "artifact" {
					return errors.New("wrong artifact")
				}
				return nil
			}
			cfg.Restart = func(unit string) error {
				calls.Add(1)
				if !sealed.Load() || unit != cfg.Unit {
					return errors.New("unsealed target")
				}
				if fixture == "rejected" {
					return errors.New("fixture rejected before dispatch")
				}
				if fixture == "uncertain" {
					return &reliability.ExecutionUncertainError{Err: errors.New("lost acknowledgement")}
				}
				return nil
			}
			stop, err := StartRestartControl(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(stop)
			err = RequestOwnedRestart(cfg.Home, cfg.Unit, controlRevision)
			switch fixture {
			case "accepted", "current":
				if err != nil {
					t.Fatal(err)
				}
			case "uncertain":
				if !reliability.IsExecutionUncertainError(err) {
					t.Fatalf("uncertainty lost: %v", err)
				}
			default:
				if !errors.Is(err, ErrRestartDeferred) {
					t.Fatalf("rejection lost: %v", err)
				}
			}
			wantCalls, wantProbes := int64(0), int64(0)
			if fixture == "artifact" {
				wantProbes = 1
			}
			if fixture == "accepted" || fixture == "rejected" || fixture == "uncertain" {
				wantCalls, wantProbes = 1, 1
			}
			if calls.Load() != wantCalls || probes.Load() != wantProbes || sealed.Load() != (fixture == "accepted" || fixture == "uncertain") {
				t.Fatalf("calls=%d probes=%d sealed=%v", calls.Load(), probes.Load(), sealed.Load())
			}
			if sealed.Load() {
				if err := RequestOwnedRestart(cfg.Home, cfg.Unit, controlRevision); !errors.Is(err, ErrRestartDeferred) || calls.Load() != 1 {
					t.Fatalf("accepted/uncertain handoff was repeated: calls=%d err=%v", calls.Load(), err)
				}
			}
		})
	}
}

func TestRestartControlRejectsMalformedRequestsAndWrongPeer(t *testing.T) {
	home := t.TempDir()
	cfg := RestartControlConfig{Home: home, Unit: "bt-dashboard", BinaryPath: "fixture-bin", Enabled: true, VerifyOwner: func(string) error { return nil }, BeginRestart: func() (func(bool), bool) { t.Error("malformed request admitted"); return nil, false }}
	stop, err := StartRestartControl(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	addr, err := restartControlAddress(home, cfg.Unit)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"revision":"bad"}`, `{"revision":"` + controlRevision + `","unit":"bt-agent"}`, `{"revision":"` + controlRevision + `"}{}`, `{"revision":"` + controlRevision + `"}` + strings.Repeat(" ", 1024)} {
		conn, err := net.DialUnix("unix", nil, addr)
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyRestartPeerUID(conn, os.Geteuid()+1); err == nil {
			t.Fatal("wrong peer identity accepted")
		}
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		_, _ = conn.Write([]byte(body))
		_ = conn.CloseWrite()
		var response restartControlResponse
		if err := json.NewDecoder(conn).Decode(&response); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
		if response.Status != "rejected" {
			t.Fatalf("malformed request accepted: %+v", response)
		}
	}
	stop()
	if err := RequestOwnedRestart(home, cfg.Unit, controlRevision); err == nil {
		t.Fatal("missing owner accepted")
	}
	if _, err := restartControlAddress(home, "../bt-dashboard"); err == nil {
		t.Fatal("unconfigured unit accepted")
	}
}

func TestRestartArtifactRequiresExactCleanIdentity(t *testing.T) {
	for _, output := range []string{
		"bt-dashboard revision=" + controlRevision + " vcs_time=unknown dirty=false",
		"bt-dashboard revision=" + controlRevision + " dirty=true",
		"bt-dashboard revision=" + strings.Repeat("a", 40) + " dirty=false",
		"bt-agent revision=" + controlRevision + " dirty=false",
		"bt-dashboard revision=" + controlRevision + " dirty=true dirty=false",
		"bt-dashboard revision=" + controlRevision + " dirty=false malformed",
	} {
		path := filepath.Join(t.TempDir(), "fixture-version")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '"+output+"'\n"), 0700); err != nil {
			t.Fatal(err)
		}
		err := verifyRestartArtifact(path, "bt-dashboard", controlRevision)
		if (err == nil) != strings.Contains(output, "vcs_time=unknown") {
			t.Fatalf("output=%q err=%v", output, err)
		}
	}
}

func TestSystemdCommandFailureAfterStartRetainsUncertainty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", root)
	if err := defaultDriftRestart("bt-dashboard"); err == nil || reliability.IsExecutionUncertainError(err) {
		t.Fatalf("identity query failure must reject: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "systemctl"), []byte("#!/bin/sh\nprintf '%s\n' '0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := defaultDriftRestart("bt-dashboard"); err == nil || reliability.IsExecutionUncertainError(err) {
		t.Fatalf("another/inactive unit must reject: %v", err)
	}
	script := "#!/bin/sh\nif [ \"$2\" = show ]; then printf '%s\n' '" + fmt.Sprint(os.Getpid()) + "'; exit 0; fi\nexit 1\n"
	if err := os.WriteFile(filepath.Join(root, "systemctl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := defaultDriftRestart("bt-dashboard"); !reliability.IsExecutionUncertainError(err) {
		t.Fatalf("started restart command failure reopened admission: %v", err)
	}
}

func TestRestartControlLostReplyAndPanicRetainOwnerSeal(t *testing.T) {
	for _, fixture := range []string{"lost-reply", "panic"} {
		t.Run(fixture, func(t *testing.T) {
			var gate reliability.RestartAdmissionGate
			var calls atomic.Int64
			called := make(chan struct{})
			cfg := RestartControlConfig{Home: t.TempDir(), Unit: "bt-dashboard", BinaryPath: "fixture-bin", Enabled: true, VerifyOwner: func(string) error { return nil },
				BeginRestart: func() (func(bool), bool) { return gate.BeginRestart(nil) },
				VerifyArtifact: func(_, _, _ string) error {
					if fixture == "panic" {
						panic("fixture verification panic")
					}
					return nil
				},
				Restart: func(string) error { calls.Add(1); close(called); return nil },
			}
			stop, err := StartRestartControl(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(stop)
			if fixture == "panic" {
				if err := RequestOwnedRestart(cfg.Home, cfg.Unit, controlRevision); !reliability.IsExecutionUncertainError(err) {
					t.Fatalf("panic was falsely rejected: %v", err)
				}
			} else {
				addr, err := restartControlAddress(cfg.Home, cfg.Unit)
				if err != nil {
					t.Fatal(err)
				}
				conn, err := net.DialUnix("unix", nil, addr)
				if err != nil {
					t.Fatal(err)
				}
				if err := json.NewEncoder(conn).Encode(restartControlRequest{Revision: controlRevision}); err != nil {
					t.Fatal(err)
				}
				_ = conn.CloseWrite()
				_ = conn.Close()
				select {
				case <-called:
				case <-time.After(time.Second):
					t.Fatal("owner did not receive complete request")
				}
			}
			if _, err := gate.Acquire(); !errors.Is(err, reliability.ErrRestartPending) {
				t.Fatalf("lost/uncertain outcome reopened admission: %v", err)
			}
			if err := RequestOwnedRestart(cfg.Home, cfg.Unit, controlRevision); !errors.Is(err, ErrRestartDeferred) {
				t.Fatalf("seal did not prevent repeat: %v", err)
			}
			want := int64(0)
			if fixture == "lost-reply" {
				want = 1
			}
			if calls.Load() != want {
				t.Fatalf("restart repeated: %d", calls.Load())
			}
		})
	}
}
