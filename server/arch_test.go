package server_test

import "github.com/bolgov0zero/linager/internal/release"

// The fixtures are amd64 releases without an assets map (the format before arm64 support).
func init() { release.HostArch = release.ArchAMD64 }
