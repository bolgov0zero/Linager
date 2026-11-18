//go:build windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Ways to free space on Windows. Each has a fixed list of places; nothing the person put there is touched: these
// are temporary files, caches that Windows builds again, the recycle bin and the leftovers of an old Windows.

func sysRoot() string {
	if v := os.Getenv("SystemRoot"); v != "" {
		return v
	}
	return `C:\Windows`
}

func sysDrive() string {
	if v := os.Getenv("SystemDrive"); v != "" {
		return v + `\`
	}
	return `C:\`
}

// winCleanDirs are the folders (or single files) of one kind of cleanup that exist on this computer.
func winCleanDirs(kind string) []string {
	var c []string
	switch kind {
	case "win-temp":
		c = append(c, filepath.Join(sysRoot(), "Temp"))
		users, _ := filepath.Glob(filepath.Join(sysDrive(), "Users", "*"))
		for _, u := range users {
			c = append(c, filepath.Join(u, "AppData", "Local", "Temp"))
		}
	case "win-update-cache":
		c = append(c, filepath.Join(sysRoot(), "SoftwareDistribution", "Download"))
	case "win-recycle":
		for _, v := range volumes() {
			c = append(c, v.mount+`\$Recycle.Bin`)
		}
	case "win-delivery":
		c = append(c, filepath.Join(sysRoot(), "ServiceProfiles", "NetworkService", "AppData", "Local", "Microsoft", "Windows", "DeliveryOptimization", "Cache"))
	case "win-dumps":
		c = append(c, filepath.Join(sysRoot(), "Minidump"), filepath.Join(sysRoot(), "MEMORY.DMP"),
			filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "WER", "ReportArchive"),
			filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "WER", "ReportQueue"))
	case "win-old":
		c = append(c, filepath.Join(sysDrive(), "Windows.old"))
	}
	var out []string
	for _, p := range c {
		if _, err := os.Lstat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

var winCleanKinds = []string{"win-temp", "win-update-cache", "win-recycle", "win-delivery", "win-dumps", "win-old"}

func pathSize(ctx context.Context, p string) uint64 {
	info, err := os.Lstat(p)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return uint64(info.Size()) //nolint:gosec // a size is not negative
	}
	cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	sc := &scanner{ctx: cctx}
	return sc.walk(p, diskMaxDepth+1).Size // deeper than the tree is kept: only the size is wanted
}

// winCleanCandidates lists what could be cleaned with its size; kinds with nothing to free are left out.
func winCleanCandidates(ctx context.Context) []cleanItem {
	var out []cleanItem
	for _, k := range winCleanKinds {
		dirs := winCleanDirs(k)
		if len(dirs) == 0 {
			continue
		}
		var size uint64
		for _, d := range dirs {
			size += pathSize(ctx, d)
		}
		if size > 1<<20 {
			out = append(out, cleanItem{Kind: k, Size: size, Frees: size, Count: len(dirs)})
		}
	}
	return out
}

// emptyDir removes what is inside a folder; files in use stay.
func emptyDir(p string) {
	info, err := os.Lstat(p)
	if err != nil {
		return
	}
	if !info.IsDir() {
		_ = os.Remove(p)
		return
	}
	ents, _ := os.ReadDir(p)
	for _, e := range ents {
		_ = os.RemoveAll(filepath.Join(p, e.Name()))
	}
}

func (m *diskModule) cleanCmd(ctx context.Context, raw json.RawMessage, dangerous bool) (any, error) {
	if !dangerous {
		return nil, errDangerous
	}
	var p struct{ Kind string }
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, errors.New("invalid request")
	}
	ok := false
	for _, k := range winCleanKinds {
		ok = ok || k == p.Kind
	}
	if !ok {
		return nil, errors.New("unknown kind of cleanup")
	}
	dirs := winCleanDirs(p.Kind)
	if len(dirs) == 0 {
		return nil, errors.New("there is nothing to clean")
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	var before uint64
	for _, d := range dirs {
		before += pathSize(ctx, d)
	}
	var output string
	switch p.Kind {
	case "win-update-cache":
		// the downloads are in use while Windows Update runs: it is stopped for the moment and started again
		out, err := runPS(ctx, `$svc = Get-Service wuauserv, bits; $was = @($svc | Where-Object { $_.Status -eq 'Running' } | ForEach-Object { $_.Name })
Stop-Service wuauserv, bits -Force -ErrorAction SilentlyContinue
Get-ChildItem -LiteralPath $env:LINAGER_DIR -Force -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
foreach ($n in $was) { Start-Service $n -ErrorAction SilentlyContinue }
'ok'`, map[string]string{"DIR": dirs[0]})
		if err != nil {
			return nil, errors.New("cleanup failed: " + err.Error())
		}
		output = string(out)
	case "win-old":
		out, err := runPS(ctx, `$d = $env:LINAGER_DIR
& takeown.exe /F $d /R /D Y | Out-Null
& icacls.exe $d /grant 'Administrators:(F)' /T /C /Q | Out-Null
Remove-Item -LiteralPath $d -Recurse -Force -ErrorAction SilentlyContinue
if (Test-Path -LiteralPath $d) { 'Some files stay (in use).' } else { 'ok' }`, map[string]string{"DIR": dirs[0]})
		if err != nil {
			return nil, errors.New("cleanup failed: " + err.Error())
		}
		output = strings.TrimSpace(string(out))
	default:
		for _, d := range dirs {
			emptyDir(d)
		}
	}
	var after uint64
	for _, d := range winCleanDirs(p.Kind) {
		after += pathSize(ctx, d)
	}
	m.forget()
	return map[string]any{"ok": true, "freed": before - min(before, after), "output": output}, nil
}
