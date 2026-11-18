package agent

import "strings"

// criticalWinServices are the Windows services whose stop, restart or disable can cut the host or the agent off.
var criticalWinServices = map[string]bool{
	"linageragent": true, "termservice": true, "sshd": true, "winrm": true, "rpcss": true, "rpceptmapper": true, "dcomlaunch": true,
	"lanmanserver": true, "lanmanworkstation": true, "dhcp": true, "dnscache": true, "nlasvc": true, "netlogon": true,
	"bfe": true, "mpssvc": true, "eventlog": true, "winmgmt": true, "cryptsvc": true, "samss": true,
}

// IsDangerousWinService reports whether an action on a Windows service needs an explicit confirmation.
func IsDangerousWinService(name, action string) bool {
	return criticalWinServices[strings.ToLower(name)] && (action == "stop" || action == "restart" || action == "disable")
}
