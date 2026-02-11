package store

import "errors"

var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("version conflict")
	ErrActiveBranch = errors.New("cannot delete the active branch")
)
