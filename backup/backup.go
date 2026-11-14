// Package backup creates and prunes Linager backups (database dump plus configuration and keys).
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultDir is where backups are kept.
const DefaultDir = "/var/backups/linager"

// Options describe one backup run.
type Options struct {
	DSN        string   // PostgreSQL DSN of the Linager database
	OutDir     string   // destination directory (created 0700)
	Files      []string // configuration and key files to include
	DataDir    string   // data directory whose keys are included
	Keep       int      // keep this many newest backups (0 = keep all)
	Prefix     string   // file name prefix, default "linager-backup"
	DumpFunc   func(ctx context.Context, dsn, dst string) error
	Now        func() time.Time
	ExtraNotes string
}

// PgDump runs pg_dump in custom format. The password is passed in the environment, not on the command line.
func PgDump(ctx context.Context, dsn, dst string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}
	args := []string{"--format=custom", "--no-owner", "--file=" + dst}
	if h := u.Hostname(); h != "" {
		args = append(args, "--host="+h)
	}
	if p := u.Port(); p != "" {
		args = append(args, "--port="+p)
	}
	if n := u.User.Username(); n != "" {
		args = append(args, "--username="+n)
	}
	args = append(args, strings.TrimPrefix(u.Path, "/"))
	cmd := exec.CommandContext(ctx, "pg_dump", args...) //nolint:gosec // arguments are parsed from the operator's own config
	cmd.Env = os.Environ()
	if pw, ok := u.User.Password(); ok {
		cmd.Env = append(cmd.Env, "PGPASSWORD="+pw)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_dump: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func addFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path) //nolint:gosec // files chosen by the backup procedure
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: int64(st.Mode().Perm()), Size: st.Size(), ModTime: st.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// Run creates the archive and returns its path.
func Run(ctx context.Context, o Options) (string, error) {
	if o.OutDir == "" {
		o.OutDir = DefaultDir
	}
	if o.Prefix == "" {
		o.Prefix = "linager-backup"
	}
	if o.DumpFunc == nil {
		o.DumpFunc = PgDump
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	if err := os.MkdirAll(o.OutDir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(o.OutDir, ".tmp-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dump := filepath.Join(tmp, "db.dump")
	if err := o.DumpFunc(ctx, o.DSN, dump); err != nil {
		return "", err
	}
	final := filepath.Join(o.OutDir, fmt.Sprintf("%s-%s.tar.gz", o.Prefix, now().Format("20060102-150405")))
	out, err := os.OpenFile(final+".part", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // backup destination
	if err != nil {
		return "", err
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	werr := addFile(tw, dump, "db.dump")
	for _, f := range o.Files {
		if werr == nil {
			if _, serr := os.Stat(f); serr == nil {
				werr = addFile(tw, f, "config/"+filepath.Base(f))
			}
		}
	}
	if ents, derr := os.ReadDir(o.DataDir); derr == nil && o.DataDir != "" {
		for _, e := range ents {
			if werr == nil && !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				werr = addFile(tw, filepath.Join(o.DataDir, e.Name()), "data/"+e.Name())
			}
		}
	}
	for _, c := range []interface{ Close() error }{tw, gz, out} {
		if cerr := c.Close(); werr == nil {
			werr = cerr
		}
	}
	if werr != nil {
		_ = os.Remove(final + ".part")
		return "", werr
	}
	if err := os.Rename(final+".part", final); err != nil {
		return "", err
	}
	if o.Keep > 0 {
		Prune(o.OutDir, o.Prefix, o.Keep)
	}
	return final, nil
}

// List returns backup files newest first.
func List(dir, prefix string) []os.FileInfo {
	if prefix == "" {
		prefix = "linager-backup"
	}
	ents, _ := os.ReadDir(dir)
	var out []os.FileInfo
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), prefix+"-") && strings.HasSuffix(e.Name(), ".tar.gz") {
			if fi, err := e.Info(); err == nil {
				out = append(out, fi)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() > out[j].Name() })
	return out
}

// Prune deletes all but the newest keep backups.
func Prune(dir, prefix string, keep int) {
	all := List(dir, prefix)
	for i := keep; i < len(all); i++ {
		_ = os.Remove(filepath.Join(dir, all[i].Name()))
	}
}

// ErrNoBackup is returned when none exists.
var ErrNoBackup = errors.New("no backup found")

// Last returns the newest backup's name and modification time.
func Last(dir, prefix string) (string, time.Time, error) {
	l := List(dir, prefix)
	if len(l) == 0 {
		return "", time.Time{}, ErrNoBackup
	}
	return l[0].Name(), l[0].ModTime(), nil
}
