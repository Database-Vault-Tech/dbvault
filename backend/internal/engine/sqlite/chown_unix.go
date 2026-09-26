//go:build unix

package sqlite

import (
	"os"
	"syscall"
)

// chownLike gives p the owner of like (best effort: only works when the
// worker runs as root or already owns the file).
func chownLike(p string, like os.FileInfo) {
	if st, ok := like.Sys().(*syscall.Stat_t); ok {
		_ = os.Chown(p, int(st.Uid), int(st.Gid))
	}
}
