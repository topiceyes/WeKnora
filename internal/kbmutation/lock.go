// Package kbmutation coordinates knowledge-base mutations with workspace moves.
// All processes must use the same lock-capable storage volume.
package kbmutation

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrBusy = errors.New("knowledge base is busy")
var ErrUnsupported = errors.New("knowledge base mutation locking is unsupported")

// Acquire obtains a non-blocking shared writer lock or an exclusive transfer
// lock. The caller must hold it for the entire operation, including file I/O.
func Acquire(baseDir, kbID string, exclusive bool) (func() error, error) {
	kbID = strings.TrimSpace(kbID)
	if kbID == "" {
		return nil, errors.New("knowledge base ID is empty")
	}
	if strings.TrimSpace(baseDir) == "" {
		return nil, errors.New("knowledge base lock directory is empty")
	}
	key := sha256.Sum256([]byte("weknora-kb-workspace-transfer:" + kbID))
	dir := filepath.Join(baseDir, ".kb-transfer-locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create knowledge base lock directory: %w", err)
	}
	return acquireFile(filepath.Join(dir, fmt.Sprintf("%x.lock", key)), exclusive)
}
