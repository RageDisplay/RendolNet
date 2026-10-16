//go:build linux

package api

import "syscall"

func diskUsage(p string) (total, used uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0, 0, err
	}
	return st.Blocks * uint64(st.Bsize), (st.Blocks - st.Bfree) * uint64(st.Bsize), nil
}
