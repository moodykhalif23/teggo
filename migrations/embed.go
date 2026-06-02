package migrations

import "embed"

// FS holds all SQL migration files, applied in lexical filename order.
//
//go:embed *.sql
var FS embed.FS
