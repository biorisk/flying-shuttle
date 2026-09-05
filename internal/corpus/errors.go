package corpus

import "errors"

// ErrNotFound is returned when a lookup by id resolves nothing.
var ErrNotFound = errors.New("not found")
