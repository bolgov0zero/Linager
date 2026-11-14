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
	"strings"
	"syscall"
)

// pgConn turns a DSN into client arguments (host, port, user), the environment (password) and the database name.
func pgConn(dsn string) (args, env []string, db string, err error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, nil, "", err
	}
	if h := u.Hostname(); h != "" {
		args = append(args, "--host="+h)
	}
	if p := u.Port(); p != "" {
		args = append(args, "--port="+p)
	}
	if n := u.User.Username(); n != "" {
		args = append(args, "--username="+n)
	}
	env = os.Environ()
	if pw, ok := u.User.Password(); ok {
		env = append(env, "PGPASSWORD="+pw)
	}
	return args, env, strings.TrimPrefix(u.Path, "/"), nil
}

// dropAll removes every table and sequence of the public schema. Migrations of a newer version may have
// added tables the dump does not know; leaving them would break the next update.
const dropAll = `DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT c.oid::regclass AS n FROM pg_class c JOIN pg_namespace ns ON ns.oid = c.relnamespace
           WHERE ns.nspname = 'public' AND c.relkind IN ('r', 'p') AND NOT c.relispartition LOOP
    EXECUTE 'DROP TABLE IF EXISTS ' || r.n || ' CASCADE';
  END LOOP;
  FOR r IN SELECT c.oid::regclass AS n FROM pg_class c JOIN pg_namespace ns ON ns.oid = c.relnamespace
           WHERE ns.nspname = 'public' AND c.relkind = 'S' LOOP
    EXECUTE 'DROP SEQUENCE IF EXISTS ' || r.n || ' CASCADE';
  END LOOP;
END $$;
`

// extract copies one archive entry into dst (mode 0600). It reports whether the entry exists.
func extract(archive, entry, dst string) (bool, error) {
	f, err := os.Open(archive) //nolint:gosec // operator-supplied backup archive
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return false, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if h.Name != entry || h.Typeflag != tar.TypeReg {
			continue
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // our own temp path
		if err != nil {
			return false, err
		}
		_, err = io.Copy(out, tr) //nolint:gosec // the archive is the operator's own backup
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		return true, err
	}
}

// RestoreDB replaces the contents of the Linager database with the dump from a backup archive.
// Everything happens in one transaction: on any error the database stays exactly as it was.
// The server must be stopped while this runs.
func RestoreDB(ctx context.Context, archive, dsn string) error {
	conn, env, db, err := pgConn(dsn)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(archive), ".restore-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	dump, script, prelude := filepath.Join(tmp, "db.dump"), filepath.Join(tmp, "db.sql"), filepath.Join(tmp, "drop.sql")
	if ok, err := extract(archive, "db.dump", dump); err != nil {
		return err
	} else if !ok {
		return errors.New("the archive has no database dump")
	}
	if out, err := exec.CommandContext(ctx, "pg_restore", "--no-owner", "--no-acl", "--file="+script, dump).CombinedOutput(); err != nil { //nolint:gosec // fixed command, our own temp paths
		return fmt.Errorf("pg_restore: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.WriteFile(prelude, []byte(dropAll), 0o600); err != nil {
		return err
	}
	args := append([]string{"--quiet", "--no-psqlrc", "--set=ON_ERROR_STOP=1", "--single-transaction", "--file=" + prelude, "--file=" + script, "--dbname=" + db}, conn...)
	cmd := exec.CommandContext(ctx, "psql", args...) //nolint:gosec // arguments are parsed from the operator's own config
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("psql: %w: %s", err, lastLines(string(out), 3))
	}
	return nil
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, " | ")
}

// RestoreKeys puts the key files of the archive (CA, certificates, signing keys) back into dataDir,
// owned like the directory itself. It returns the restored file names.
func RestoreKeys(archive, dataDir string) ([]string, error) {
	st, err := os.Stat(dataDir)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(archive) //nolint:gosec // operator-supplied backup archive
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var names []string
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return names, err
		}
		name, isData := strings.CutPrefix(h.Name, "data/")
		if !isData || h.Typeflag != tar.TypeReg || name == "" || name != filepath.Base(name) || strings.HasSuffix(name, ".json") {
			continue // only plain files of the data directory; status/request files are not keys
		}
		dst := filepath.Join(dataDir, name)
		tmp := dst + ".restore"
		_ = os.Remove(tmp)
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // inside the data directory
		if err != nil {
			return names, err
		}
		_, err = io.Copy(out, tr) //nolint:gosec // the archive is the operator's own backup
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			if sys, ok := st.Sys().(*syscall.Stat_t); ok {
				_ = os.Lchown(tmp, int(sys.Uid), int(sys.Gid))
			}
			err = os.Rename(tmp, dst)
		}
		if err != nil {
			_ = os.Remove(tmp)
			return names, err
		}
		names = append(names, name)
	}
}
