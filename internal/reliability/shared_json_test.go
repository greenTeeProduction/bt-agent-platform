package reliability

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSharedJSONConcurrentTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "counter.json")
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if err := UpdateSharedJSON(path, func(data []byte) (any, error) {
				var count int
				if len(data) > 0 {
					if err := json.Unmarshal(data, &count); err != nil {
						return nil, err
					}
				}
				return count + 1, nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := json.Unmarshal(data, &count); err != nil {
		t.Fatal(err)
	}
	if count != 20 {
		t.Fatalf("lost updates: %d", count)
	}
}

func TestSharedJSONFailurePreservesCommittedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("17"), 0600); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("callback failed")
	if err := UpdateSharedJSON(path, func([]byte) (any, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("err=%v", err)
	}
	release, err := AcquireFileLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if err := UpdateSharedJSONWithContext(ctx, path, func([]byte) (any, error) { called = true; return 99, nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if called {
		t.Fatal("canceled waiter reached callback")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "17" {
		t.Fatalf("committed value=%q err=%v", data, err)
	}
}
