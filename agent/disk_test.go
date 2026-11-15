package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestForecast(t *testing.T) {
	now := int64(1_800_000_000)
	var pts []histPoint
	for i := 0; i < 48; i++ { // two days, a point every hour, 1 GiB per day
		pts = append(pts, histPoint{T: now - int64(47-i)*3600, U: 10<<30 + uint64(i)*(1<<30)/24})
	}
	perDay, left, span := forecast(pts, 10<<30, now)
	if perDay < 0.95*(1<<30) || perDay > 1.05*(1<<30) {
		t.Fatalf("growth per day: %f", perDay)
	}
	if left < 9 || left > 11 || span < 1.9 {
		t.Fatalf("days left %f span %f", left, span)
	}
	if p, l, _ := forecast(pts[:3], 1<<30, now); p != 0 || l != 0 {
		t.Fatal("too few points give no forecast")
	}
	flat := []histPoint{}
	for i := 0; i < 20; i++ {
		flat = append(flat, histPoint{T: now - int64(20-i)*3600, U: 5 << 30})
	}
	if _, l, _ := forecast(flat, 1<<30, now); l != 0 {
		t.Fatal("a filesystem that does not grow never fills up")
	}
	tr := trend(pts, 30)
	if len(tr) != 3 && len(tr) != 2 {
		t.Fatalf("one value per day: %v", tr)
	}
}

func TestParseJournalUsageAndKernels(t *testing.T) {
	if n := parseJournalUsage("Archived and active journals take up 2.1G in the file system.\n"); n < 2_200_000_000 || n > 2_300_000_000 {
		t.Fatalf("%d", n)
	}
	if parseJournalUsage("nothing") != 0 {
		t.Fatal("no number")
	}
	pkgs := []kernelPkg{
		{name: "linux-image-6.1.0-17-amd64", size: 300 << 20},
		{name: "linux-image-6.1.0-18-amd64", size: 300 << 20},
		{name: "linux-image-6.1.0-9-amd64", size: 300 << 20},
		{name: "linux-image-amd64"}, {name: "linux-image-6.1.0-17-amd64-dbg"},
	}
	old := oldKernels(pkgs, "6.1.0-17-amd64")
	if len(old) != 1 || old[0].name != "linux-image-6.1.0-9-amd64" {
		t.Fatalf("the running and the newest kernels stay (numbers compare as numbers): %+v", old)
	}
	if len(oldKernels(pkgs[:1], "6.1.0-17-amd64")) != 0 {
		t.Fatal("a single kernel stays")
	}
}

func TestScanAndCategorize(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, size int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("var/log/a.log", 200_000)
	write("var/lib/docker/x", 900_000)
	write("var/lib/postgresql/16/base", 400_000)
	write("home/alice/f", 100_000)
	write("opt/big", 50_000)
	if err := os.Symlink("/", filepath.Join(root, "loop")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "opt/big"), filepath.Join(root, "opt/big2")); err != nil {
		t.Fatal(err)
	}

	m := newDiskModule(t.TempDir())
	m.filesystems = func() []fsInfo {
		return []fsInfo{{mount: root, device: "/dev/x", fs: "ext4", total: 10 << 20, used: 4 << 20, avail: 5 << 20}}
	}
	res, err := m.scanCmd(context.Background(), json.RawMessage(`{"mount":"`+root+`"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	sc := res.(*diskScan)
	if sc.Partial || sc.Tree.Size < 1_600_000 {
		t.Fatalf("partial=%v size=%d", sc.Partial, sc.Tree.Size)
	}
	by := map[string]uint64{}
	for _, c := range sc.Categories {
		by[c.ID] = c.Size
	}
	if by["docker"] < 900_000 || by["docker"] > 1_000_000 || by["logs"] < 200_000 || by["db"] < 400_000 || by["home"] < 100_000 {
		t.Fatalf("categories: %v", by)
	}
	if by["free"] != 5<<20 || by["reserved"] != 1<<20 {
		t.Fatalf("free and reserved: %v", by)
	}
	if n := findNode(sc.Tree, "/var/lib/docker"); n == nil || n.Size < 900_000 {
		t.Fatalf("the docker node must be kept: %+v", n)
	}
	// a hard link is counted once
	if n := findNode(sc.Tree, "/opt"); n == nil || n.Size > 60_000+4096*2 {
		t.Fatalf("opt: %+v", n)
	}
	if _, err := m.scanCmd(context.Background(), json.RawMessage(`{"mount":"/etc"}`), false); err == nil {
		t.Fatal("only a mount the agent lists can be scanned")
	}
	again, _ := m.scanCmd(context.Background(), json.RawMessage(`{"mount":"`+root+`"}`), false)
	if again.(*diskScan) != sc {
		t.Fatal("a fresh scan is answered from the cache")
	}
}

func TestRecordKeepsHistory(t *testing.T) {
	dir := t.TempDir()
	m := newDiskModule(dir)
	now := time.Unix(1_800_000_000, 0)
	m.now = func() time.Time { return now }
	used := uint64(1 << 30)
	m.filesystems = func() []fsInfo { return []fsInfo{{mount: "/", total: 10 << 30, used: used, avail: 5 << 30}} }
	m.record()
	m.record() // too soon: no second point
	if len(m.hist["/"]) != 1 {
		t.Fatalf("%v", m.hist)
	}
	now = now.Add(31 * time.Minute)
	m.record()
	if len(m.hist["/"]) != 2 {
		t.Fatalf("%v", m.hist)
	}
	if st, err := os.Stat(filepath.Join(dir, "disk-history.json")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the history file must be private: %v %v", st, err)
	}
	m2 := newDiskModule(dir)
	if len(m2.hist["/"]) != 2 {
		t.Fatal("the history survives a restart")
	}
	m.filesystems = func() []fsInfo { return nil }
	m.record()
	if len(m.hist) != 0 {
		t.Fatal("an unmounted filesystem loses its history")
	}
}

func TestCleanCmdValidation(t *testing.T) {
	m := newDiskModule(t.TempDir())
	m.filesystems = func() []fsInfo { return nil }
	var ran []string
	m.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		if name == "dpkg-query" {
			return []byte("ii \tlinux-image-6.1.0-9-amd64\t6.1.0-9\t300000\nii \tlinux-image-6.1.0-18-amd64\t6.1.0-18\t300000\nii \tlinux-image-6.1.0-17-amd64\t6.1.0-17\t300000\n"), nil
		}
		if name == "uname" {
			return []byte("6.1.0-17-amd64\n"), nil
		}
		return nil, nil
	}
	ctx := context.Background()
	if _, err := m.cleanCmd(ctx, json.RawMessage(`{"kind":"apt"}`), false); !errors.Is(err, errDangerous) {
		t.Fatalf("needs a confirmation: %v", err)
	}
	if _, err := m.cleanCmd(ctx, json.RawMessage(`{"kind":"rm -rf /"}`), true); err == nil {
		t.Fatal("an unknown kind is refused")
	}
	if len(ran) != 0 {
		t.Fatalf("a refused request runs nothing: %v", ran)
	}
	if _, err := m.cleanCmd(ctx, json.RawMessage(`{"kind":"kernels"}`), true); err != nil {
		t.Fatal(err)
	}
	last := ran[len(ran)-1]
	if last != "apt-get purge -y -o DPkg::Lock::Timeout=120 linux-image-6.1.0-9-amd64" {
		t.Fatalf("only the old kernel goes: %q", last)
	}
	if _, err := m.cleanCmd(ctx, json.RawMessage(`{"kind":"journal"}`), true); err != nil {
		t.Fatal(err)
	}
	if ran[len(ran)-1] != "journalctl --vacuum-size=500M" {
		t.Fatal(ran)
	}
}
