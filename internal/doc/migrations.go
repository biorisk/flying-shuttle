package doc

import "embed"

//go:embed migrations/*.sql
var migrationFS embed.FS
