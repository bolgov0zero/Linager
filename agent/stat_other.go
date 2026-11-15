//go:build !linux

package agent

import "syscall"

// statTimes is the development fallback for non-Linux hosts (the agent itself only runs on Linux).
func statTimes(s *syscall.Stat_t) (atime, ctime int64) {
	return s.Atimespec.Sec, s.Ctimespec.Sec
}
