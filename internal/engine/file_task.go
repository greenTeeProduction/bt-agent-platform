package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	btcore "github.com/rvitorper/go-bt/core"
	btleaf "github.com/rvitorper/go-bt/leaf"
)

// ArtifactRootFn is wired by the owner-aware runtime. Definitions cannot
// choose an absolute filesystem root, and an unwired capability fails closed.
var ArtifactRootFn func(user string) (string, error)

const artifactSizeLimit = 1024 * 1024

func artifactDigest(data []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(data)) }

func readTaskArtifact(root *os.Root, name string) ([]byte, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > artifactSizeLimit {
		return nil, fmt.Errorf("artifact must be a regular file of at most 1 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, artifactSizeLimit+1))
	if len(data) > artifactSizeLimit {
		return nil, fmt.Errorf("artifact exceeds 1 MiB")
	}
	return data, err
}

func writeTaskArtifact(root *os.Root, name string, data []byte) error {
	if err := root.MkdirAll(path.Dir(name), 0750); err != nil {
		return err
	}
	temp := path.Join(path.Dir(name), ".artifact-"+rand.Text()+".tmp")
	f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(temp) }()
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Rename(temp, name)
}

func verifyTaskArtifact(root *os.Root, spec evolution.FileTaskSpec, expected []byte, contract *evolution.ResultContract) error {
	actual, err := readTaskArtifact(root, spec.Output)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("artifact readback differs from the committed result")
	}
	return contract.Verify(string(actual))
}

// buildFileTask runs the governed child against an input snapshot, then
// commits and independently reads the output. It caches terminal disposition
// within this run; a retry cannot repeat a completed or uncertain write.
func buildFileTask(node *evolution.SerializableNode, bb *Blackboard) btcore.Command[Blackboard] {
	spec, parseErr := evolution.ParseFileTask(node)
	var child btcore.Command[Blackboard]
	if parseErr == nil {
		child = buildNode(&node.Children[0], bb, node.Name)
	}
	contract, _ := evolution.ParseResultContract(node)
	owner, _ := node.Metadata["user"].(string)
	task, _ := node.Metadata["task"].(string)
	var run, rootPath string
	var input []byte
	prepared, terminal := false, 0
	return btleaf.NewAction(func(ctx *btcore.BTContext[Blackboard]) int {
		b := ctx.Blackboard
		if b.runEvidence != nil && run != b.runEvidence.id {
			run = b.runEvidence.id
			prepared, terminal = false, 0
		}
		if terminal != 0 {
			return terminal
		}
		fail := func(err error, uncertain bool) int {
			terminal = -1
			var stop error = &reliability.ExecutionStoppedError{Outcome: "failure", Err: err}
			if uncertain {
				stop = &reliability.ExecutionUncertainError{Err: err}
			}
			b.stopExecution(err.Error(), stop)
			b.applyExecutionStop()
			return terminal
		}
		if parseErr != nil {
			return fail(parseErr, false)
		}
		if b.User != owner || b.Task != task {
			return fail(fmt.Errorf("file task owner or task mismatch"), false)
		}
		if ArtifactRootFn == nil {
			return fail(fmt.Errorf("artifact storage is not configured"), false)
		}
		if err := chainContext(b).Err(); err != nil {
			return fail(err, false)
		}
		if !prepared {
			var err error
			rootPath, err = ArtifactRootFn(owner)
			if err != nil || rootPath == "" {
				return fail(fmt.Errorf("artifact root unavailable: %v", err), false)
			}
			if err := os.MkdirAll(rootPath, 0750); err != nil {
				return fail(err, false)
			}
			root, err := os.OpenRoot(rootPath)
			if err != nil {
				return fail(err, false)
			}
			if spec.Input != "" {
				input, err = readTaskArtifact(root, spec.Input)
			}
			_ = root.Close()
			if err != nil {
				return fail(err, false)
			}
			if b.ChainState == nil {
				b.ChainState = map[string]any{}
			}
			b.ChainState["task_input"] = string(input)
			prepared = true
		}
		code := child.Run(ctx)
		if code == 0 {
			return 0
		}
		if code != 1 || b.applyExecutionStop() {
			terminal = -1
			return terminal
		}
		if err := contract.Verify(b.Result); err != nil {
			return fail(err, false)
		}
		// Save actual JSON, including when the existing response contract accepts a
		// complete JSON code fence. The independent expected values stay in gates.
		text := strings.TrimSpace(b.Result)
		if strings.HasPrefix(text, "```json\n") && strings.HasSuffix(text, "```") {
			text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "```json\n"), "```"))
		}
		var payload bytes.Buffer
		if err := json.Compact(&payload, []byte(text)); err != nil {
			return fail(err, false)
		}
		data := append(payload.Bytes(), '\n')
		if err := contract.Verify(string(data)); err != nil {
			return fail(err, false)
		}
		if len(data) > artifactSizeLimit {
			return fail(fmt.Errorf("result artifact exceeds 1 MiB"), false)
		}
		lockCtx, cancel := context.WithTimeout(chainContext(b), 5*time.Second)
		defer cancel()
		release, err := reliability.AcquireFileLockWithContext(lockCtx, filepath.Join(rootPath, fmt.Sprintf(".artifact-%x", sha256.Sum256([]byte(spec.Output)))))
		if err != nil {
			return fail(err, false)
		}
		defer release()
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			return fail(err, false)
		}
		defer root.Close()
		if spec.Input != "" {
			current, err := readTaskArtifact(root, spec.Input)
			if err != nil || !bytes.Equal(current, input) {
				return fail(fmt.Errorf("task input changed or became unreadable before output commit"), false)
			}
		}
		if err := chainContext(b).Err(); err != nil {
			return fail(err, false)
		}
		receipt := evolution.EffectReceipt{Kind: "file", Input: spec.Input, Output: spec.Output, OutputDigest: artifactDigest(data)}
		if spec.Input != "" {
			receipt.InputDigest = artifactDigest(input)
		}
		if err := writeTaskArtifact(root, spec.Output, data); err != nil {
			receipt.Error = err.Error()
			b.recordEffect(receipt)
			return fail(err, false)
		}
		receipt.WriteCommitted = true
		if err := verifyTaskArtifact(root, *spec, data, contract); err != nil {
			receipt.Error = err.Error()
			b.recordEffect(receipt)
			return fail(err, true)
		}
		receipt.Verified = true
		b.recordEffect(receipt)
		b.Result, b.CachedResult = string(data), string(data)
		// The committed/read-back JSON has been normalized. Bind a check to
		// these exact final bytes, not just to the worker's earlier formatting.
		if !resultContractVerifier(node, contract)(b) {
			return fail(fmt.Errorf("committed artifact failed its final result contract"), true)
		}
		b.Outcome = "success"
		terminal = 1
		return terminal
	})
}

func (b *Blackboard) recordEffect(receipt evolution.EffectReceipt) {
	receipt.Scope = b.goapEffectScope
	if b.runEvidence == nil {
		return
	}
	b.runEvidence.mu.Lock()
	defer b.runEvidence.mu.Unlock()
	if len(b.runEvidence.effects) < maxChildTicks {
		b.runEvidence.effects = append(b.runEvidence.effects, receipt)
	} else {
		b.runEvidence.checksDropped++
	}
}

func (b *Blackboard) EvidenceEffects() []evolution.EffectReceipt {
	if b.runEvidence == nil {
		return nil
	}
	b.runEvidence.mu.Lock()
	defer b.runEvidence.mu.Unlock()
	return append([]evolution.EffectReceipt(nil), b.runEvidence.effects...)
}
