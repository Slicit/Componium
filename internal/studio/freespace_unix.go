//go:build unix

package studio

import "syscall"

// freeBytes reports space left where films are stored, since running out of it
// is the reason anybody deletes one.
func freeBytes(dir string) int64 {
	if dir == "" {
		return 0
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * int64(st.Bsize)
}
