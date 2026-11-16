package update

import "github.com/bolgov0zero/linager/internal/release"

// The fixtures of this package are amd64 releases without an assets map (the format before arm64
// support); tests of other architectures set release.HostArch themselves.
func init() { release.HostArch = release.ArchAMD64 }
