//go:build !windows

package agent

import "testing"

func TestIgnoredPackagesAreNotCounted(t *testing.T) {
	loadIgnores(t.TempDir())
	pk := []AptPackage{{Name: "openssl", Security: true}, {Name: "curl", Security: true}, {Name: "nginx"}}
	if total, sec := countApt(pk); total != 3 || sec != 2 {
		t.Fatalf("before: %d %d", total, sec)
	}
	_ = ignores.set("apt", "openssl", true)
	_ = ignores.set("apt", "nginx", true)
	if total, sec := countApt(pk); total != 1 || sec != 1 {
		t.Fatalf("ignored packages must stay out of the numbers, got %d %d", total, sec)
	}
}

func TestIgnoredContainersAreNotCounted(t *testing.T) {
	loadIgnores(t.TempDir())
	entries := map[string]checkEntry{
		"a": {name: "web", status: "new"},
		"b": {name: "db", status: "new"},
		"c": {name: "cache", status: "current"},
	}
	if n := countNewImages(entries); n != 2 {
		t.Fatalf("before: %d", n)
	}
	_ = ignores.set("docker", "db", true)
	if n := countNewImages(entries); n != 1 {
		t.Fatalf("an ignored container must stay out of the number, got %d", n)
	}
	// the stored result is recounted without a new check
	dockerChecks.mu.Lock()
	dockerChecks.at, dockerChecks.entries, dockerChecks.updates = 1, entries, 2
	dockerChecks.mu.Unlock()
	recountImages()
	if got := imageUpdates(); got != 1 {
		t.Fatalf("recount: %d", got)
	}
	dockerChecks.mu.Lock()
	dockerChecks.at, dockerChecks.entries, dockerChecks.updates = 0, map[string]checkEntry{}, 0
	dockerChecks.mu.Unlock()
}
