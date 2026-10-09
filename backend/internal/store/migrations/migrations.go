// Package migrations embeds the SQL schema migrations.
//
// Files are named NNNN_description.up.sql and applied in lexical order. There are no down
// migrations for the MVP; reset with `make db-reset`.
package migrations

import "embed"

// FS holds every *.up.sql migration.
//
//go:embed *.up.sql
var FS embed.FS
