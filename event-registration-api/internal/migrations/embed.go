package migrations

import "embed"

// Files contains the schema history used by the migrate command and integration tests.
//
//go:embed *.sql
var Files embed.FS
