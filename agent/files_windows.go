//go:build windows

package agent

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// The panel shows every path as "/C:/Users/x": a tree that starts at "/" (the list of drives), like on Linux, so
// the file manager needs no special case. The agent also accepts "C:/x" and "C:\x".

// cleanPath validates a path and turns it into the native form ("C:\Users\x").
func cleanPath(p string) (string, error) {
	s, err := normalizeWinPath(p)
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(s), nil
}

// outPath is how a path is shown to the panel.
func outPath(p string) string { return "/" + filepath.ToSlash(p) }

// protectedNative lists the system folders that cannot be renamed, deleted or replaced.
func protectedNative() []string {
	var out []string
	for _, k := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "ProgramData", "SystemDrive"} {
		if v := os.Getenv(k); v != "" {
			out = append(out, v)
		}
	}
	if d := os.Getenv("SystemDrive"); d != "" {
		out = append(out, d+`\Users`)
	}
	return out
}

// isProtected reports whether a path is a drive root or a system folder.
func isProtected(p string) bool {
	p = strings.TrimRight(filepath.Clean(p), `\`)
	if len(p) == 2 && p[1] == ':' {
		return true // a drive root
	}
	for _, v := range protectedNative() {
		if strings.EqualFold(strings.TrimRight(filepath.Clean(v), `\`), p) {
			return true
		}
	}
	return false
}

// ownerOf names the owner of a file. Windows keeps ACLs instead of unix owners; the panel shows nothing here.
func ownerOf(fs.FileInfo) (owner, group string) { return "", "" }

// ownerIDs has no meaning on Windows.
func ownerIDs(fs.FileInfo) (uid, gid int, ok bool) { return 0, 0, false }

// keepOwner is a no-op: a copy gets the ACL of its folder.
func keepOwner(string, fs.FileInfo, bool) {}

// fillStat adds the times Windows keeps to the properties of a file.
func fillStat(info fs.FileInfo, st *FileStat, fallbackMode string) {
	st.Mode = fallbackMode
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		st.ATime = d.LastAccessTime.Nanoseconds() / 1e9
		st.CTime = d.LastWriteTime.Nanoseconds() / 1e9
	}
}

// birthTime is the creation time of a file.
func birthTime(_ context.Context, path string) int64 {
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return d.CreationTime.Nanoseconds() / 1e9
	}
	return 0
}

// isCrossDevice reports a rename between drives (ERROR_NOT_SAME_DEVICE).
func isCrossDevice(err error) bool { return errors.Is(err, syscall.Errno(0x11)) }

// listRoot lists the drives for the top of the tree.
func listRoot(p string) (any, bool) {
	if q := strings.Trim(strings.ReplaceAll(p, `\`, "/"), "/"); q != "" {
		return nil, false
	}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, false
	}
	out := []FileEntry{}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		name := string(rune('A'+i)) + ":"
		out = append(out, FileEntry{Name: name, Type: "dir", Mode: "0755", ModeStr: "d---------"})
	}
	return map[string]any{"path": "/", "entries": out}, true
}
