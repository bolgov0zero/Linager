//go:build windows

package agent

import (
	"golang.org/x/sys/windows/registry"
)

// osName describes this Windows, for example "Windows Server 2022 Standard (21H2, build 20348)".
func osName() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "Windows"
	}
	defer func() { _ = k.Close() }()
	get := func(name string) string { v, _, _ := k.GetStringValue(name); return v }
	return windowsName(get("ProductName"), firstFilled(get("DisplayVersion"), get("ReleaseId")), get("CurrentBuild"))
}

func firstFilled(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
