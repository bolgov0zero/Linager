//go:build linux

package agent

import "syscall"

// statTimes returns the access and inode change times.
func statTimes(s *syscall.Stat_t) (atime, ctime int64) { return int64(s.Atim.Sec), int64(s.Ctim.Sec) } //nolint:unconvert // the field type differs between platforms
