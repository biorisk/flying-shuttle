package project

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

// CorpusChunkCount returns the number of chunks in a corpus database, or 0 if
// the file is missing / unreadable / not yet migrated. Read-only, cheap; used
// to annotate the corpus bind picker.
func CorpusChunkCount(corpusDB string) int {
	db, err := sql.Open("sqlite", corpusDB+"?mode=ro")
	if err != nil {
		return 0
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM chunks`).Scan(&n); err != nil {
		return 0
	}
	return n
}
