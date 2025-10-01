// Package migrations embeds the numbered SQL migrations so the migrate command
// and the tests apply exactly the same files, with nothing to copy at runtime.
//
// Files are NNNNNN_name.up.sql and NNNNNN_name.down.sql (golang-migrate format).
// A migration is never edited after it is committed; changes go in a new file.
package migrations

import "embed"

// FS holds every *.sql file in this directory.
//
//go:embed *.sql
var FS embed.FS
