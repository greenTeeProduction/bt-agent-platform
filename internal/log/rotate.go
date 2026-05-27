package log

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// RotatingWriter implements io.WriteCloser with automatic file rotation.
// When the current file exceeds MaxSize, it is rotated: .log → .log.1, .log.1 → .log.2, etc.
// MaxBackups old files are kept; older files are deleted.
type RotatingWriter struct {
	mu        sync.Mutex
	file      *os.File
	filePath  string
	size      int64
	MaxSize   int64 // Max file size in bytes before rotation (default 10MB)
	MaxBackups int  // Max number of old log files to keep (default 5)
}

// NewRotatingWriter creates a new rotating writer that writes to filePath.
// If the file exists, it appends to it. The file is created if it doesn't exist.
// Defaults: MaxSize=10MB, MaxBackups=5.
func NewRotatingWriter(filePath string) (*RotatingWriter, error) {
	rw := &RotatingWriter{
		filePath:   filePath,
		MaxSize:    10 * 1024 * 1024, // 10MB
		MaxBackups: 5,
	}

	// Open existing file for appending, or create a new one
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("rotating writer: open %s: %w", filePath, err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("rotating writer: stat %s: %w", filePath, err)
	}

	rw.file = f
	rw.size = stat.Size()
	return rw, nil
}

// Write writes p to the current log file, rotating if the file exceeds MaxSize.
func (rw *RotatingWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	// Check if we need to rotate BEFORE writing
	if rw.MaxSize > 0 && rw.size > 0 && rw.size+int64(len(p)) > rw.MaxSize {
		if err := rw.rotate(); err != nil {
			return 0, fmt.Errorf("rotating writer: rotate: %w", err)
		}
	}

	n, err := rw.file.Write(p)
	rw.size += int64(n)
	return n, err
}

// Close closes the current log file.
func (rw *RotatingWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.file != nil {
		err := rw.file.Close()
		rw.file = nil
		return err
	}
	return nil
}

// rotate closes the current file, rotates backup files, and opens a new log file.
// Must be called with rw.mu held.
func (rw *RotatingWriter) rotate() error {
	// Close current file
	if err := rw.file.Close(); err != nil {
		return fmt.Errorf("close current: %w", err)
	}

	// Rotate backup files: bt.log.N → bt.log.(N+1)
	for i := rw.MaxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", rw.filePath, i)
		newPath := fmt.Sprintf("%s.%d", rw.filePath, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			if err := os.Rename(oldPath, newPath); err != nil {
				return fmt.Errorf("rename %s -> %s: %w", oldPath, newPath, err)
			}
		}
	}

	// Rotate current file to .1
	backup1 := rw.filePath + ".1"
	if err := os.Rename(rw.filePath, backup1); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", rw.filePath, backup1, err)
	}

	// Delete oldest backup if it exceeds MaxBackups
	oldest := fmt.Sprintf("%s.%d", rw.filePath, rw.MaxBackups+1)
	_ = os.Remove(oldest) // Best effort

	// Open new log file
	f, err := os.OpenFile(rw.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open new %s: %w", rw.filePath, err)
	}

	rw.file = f
	rw.size = 0
	return nil
}

// Size returns the current file size in bytes.
func (rw *RotatingWriter) Size() int64 {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.size
}

// ─── Cleanup ─────────────────────────────────────────────────────────────────

// CleanupOldLogs removes rotated log files older than maxBackups.
// This is a utility function for cleaning up after a MaxBackups change.
func CleanupOldLogs(filePath string, maxBackups int) error {
	dir := filepath.Dir(filePath)
	base := filepath.Base(filePath)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	prefix := base + "."
	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			backups = append(backups, filepath.Join(dir, e.Name()))
		}
	}

	sort.Strings(backups)

	// Remove backups beyond maxBackups
	for i := maxBackups; i < len(backups); i++ {
		_ = os.Remove(backups[i])
	}

	return nil
}
