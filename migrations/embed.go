// Package migrations embeds the forward-only SQL migrations.
package migrations

import "embed"

// FS holds the goose migration files.
//
//go:embed *.sql
var FS embed.FS
