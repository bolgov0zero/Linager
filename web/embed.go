// Package web embeds the built frontend (web/dist) into the binary.
package web

import "embed"

// Dist holds the compiled frontend. Build it with `make web` before `go build`.
//
//go:embed all:dist
var Dist embed.FS
