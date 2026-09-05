package corpus

import "embed"

//go:embed migrations/*.sql
var migrationFS embed.FS

var migrations = []string{
	"migrations/001_corpus.sql",
	"migrations/002_uploads.sql",
	"migrations/003_atlas.sql",
	"migrations/004_soft_delete.sql",
}
