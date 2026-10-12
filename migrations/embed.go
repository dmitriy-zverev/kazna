// Package migrations embeds the SQL migration files so they can be applied
// automatically at startup without shipping the .sql files alongside the binary.
package migrations

import "embed"

// FS contains every SQL migration in this directory.
//
//go:embed *.sql
var FS embed.FS
