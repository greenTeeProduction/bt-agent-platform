package reliability

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nico/go-bt-evolve/internal/util"
)

// UpdateSharedJSON serializes a complete read/update/atomic-replace transaction.
// The default lock wait is bounded; callers with their own budget use
// UpdateSharedJSONWithContext. The callback must not acquire the same file lock.
func UpdateSharedJSON(path string, update func([]byte) (any, error)) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return UpdateSharedJSONWithContext(ctx, path, update)
}

func UpdateSharedJSONWithContext(ctx context.Context, path string, update func([]byte) (any, error)) error {
	if err := util.EnsurePersistenceParent(path); err != nil {
		return err
	}
	release, err := AcquireFileLockWithContext(ctx, path)
	if err != nil {
		return err
	}
	defer release()
	data, err := util.ReadPersistenceFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	value, err := update(data)
	if err != nil {
		return fmt.Errorf("update %s: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return util.SaveJSONAtomic(path, value)
}
