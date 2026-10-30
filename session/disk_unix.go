//go:build !windows

package session

import "syscall"

// DiskFreeGB is the free space on the disk holding path, in GB; -1 when unknown.
func DiskFreeGB(path string) float64 {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return -1
	}
	return float64(st.Bavail) * float64(st.Bsize) / (1 << 30)
}
