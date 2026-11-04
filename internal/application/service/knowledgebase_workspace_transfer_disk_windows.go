//go:build windows

package service

// localDiskFreeBytes is unavailable on Windows; the preflight skips the free
// space check rather than blocking every move.
func localDiskFreeBytes(string) (int64, bool) { return 0, false }
