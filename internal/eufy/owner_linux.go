package eufy

import (
	"io/fs"
	"syscall"
)

type ownerIDs struct{ uid, gid int }

// fileOwner reads uid/gid from a FileInfo so a rewritten config keeps the ownership
// the bridge's service user needs to read it.
func fileOwner(fi fs.FileInfo) (ownerIDs, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ownerIDs{}, false
	}
	return ownerIDs{uid: int(st.Uid), gid: int(st.Gid)}, true
}
