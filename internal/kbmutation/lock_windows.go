//go:build windows

package kbmutation

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func acquireFile(path string, exclusive bool) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	overlap := &windows.Overlapped{}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, overlap); err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, ErrBusy
		}
		return nil, err
	}
	return func() error {
		err := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, overlap)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}, nil
}
