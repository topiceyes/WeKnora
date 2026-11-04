//go:build !windows

package service

import "syscall"

// localDiskFreeBytes reports the space available to unprivileged writers on the
// volume holding path. ok is false when the platform cannot tell us.
func localDiskFreeBytes(path string) (int64, bool) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, false
	}
	return int64(stat.Bavail) * int64(stat.Bsize), true
}
