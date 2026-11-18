//go:build !windows

package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var protectedPaths = map[string]bool{"/": true, "/bin": true, "/boot": true, "/dev": true, "/etc": true, "/home": true, "/lib": true, "/lib64": true,
	"/opt": true, "/proc": true, "/root": true, "/run": true, "/sbin": true, "/sys": true, "/usr": true, "/var": true}

// cleanPath validates and normalizes an absolute path.
func cleanPath(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) || strings.ContainsRune(p, 0) || len(p) > 4096 {
		return "", errors.New("invalid path")
	}
	return filepath.Clean(p), nil
}

var (
	idMu    sync.Mutex
	uidName = map[uint32]string{}
	gidName = map[uint32]string{}
)

func lookupUser(uid uint32) string {
	idMu.Lock()
	defer idMu.Unlock()
	if n, ok := uidName[uid]; ok {
		return n
	}
	n := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(n); err == nil {
		n = u.Username
	}
	uidName[uid] = n
	return n
}

func lookupGroup(gid uint32) string {
	idMu.Lock()
	defer idMu.Unlock()
	if n, ok := gidName[gid]; ok {
		return n
	}
	n := strconv.FormatUint(uint64(gid), 10)
	if g, err := user.LookupGroupId(n); err == nil {
		n = g.Name
	}
	gidName[gid] = n
	return n
}

// isProtected reports whether a path is a system directory whose permissions, owner and name stay as they are.
func isProtected(p string) bool { return protectedPaths[p] }

// ownerOf names the owner and group of a file.
func ownerOf(info fs.FileInfo) (owner, group string) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return lookupUser(st.Uid), lookupGroup(st.Gid)
	}
	return "", ""
}

// ownerIDs returns the numeric owner and group of a file.
func ownerIDs(info fs.FileInfo) (uid, gid int, ok bool) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid), int(st.Gid), true
	}
	return 0, 0, false
}

// keepOwner gives the copy the owner of the original; it only works as root and is ignored otherwise.
func keepOwner(path string, st fs.FileInfo, link bool) {
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	if link {
		_ = os.Lchown(path, int(sys.Uid), int(sys.Gid))
		return
	}
	_ = os.Chown(path, int(sys.Uid), int(sys.Gid))
}

// fillStat adds the unix details of a file to its properties.
func fillStat(info fs.FileInfo, st *FileStat, fallbackMode string) {
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		st.Mode = fmt.Sprintf("%04o", sys.Mode&0o7777)
		st.UID, st.GID, st.Links, st.Inode = sys.Uid, sys.Gid, uint64(sys.Nlink), sys.Ino //nolint:unconvert // Nlink is not uint64 everywhere
		st.ATime, st.CTime = statTimes(sys)
	} else {
		st.Mode = fallbackMode
	}
}

// birthTime asks stat(1) for the creation time (Go's standard library does not expose it).
func birthTime(ctx context.Context, path string) int64 {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "stat", "-c", "%W", "--", path).Output() //nolint:gosec // fixed command, the path follows "--"
	if err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

// isCrossDevice reports a rename between file systems.
func isCrossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

// outPath is how a path is shown to the panel.
func outPath(p string) string { return p }

// listRoot lists the top of the tree when it is not a directory (Windows: the drives). Not needed here.
func listRoot(string) (any, bool) { return nil, false }
