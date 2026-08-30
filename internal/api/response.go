package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"

	"github.com/biorisk/flying-shuttle/internal/store"
)

type envelope struct {
	Data  any       `json:"data,omitempty"`
	Error string    `json:"error,omitempty"`
	Meta  *pageMeta `json:"meta,omitempty"`
}

// pageMeta accompanies a paginated list response.
type pageMeta struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// defaultPageLimit / maxPageLimit bound list endpoints that omit an explicit
// limit. A client may still request more up to maxPageLimit.
const (
	defaultPageLimit = 200
	maxPageLimit     = 2000
)

// parsePage reads ?limit= and ?offset= with sane defaults and clamping.
func parsePage(r *http.Request) (limit, offset int) {
	limit = defaultPageLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

// writePage emits a list plus pagination metadata.
func writePage(w http.ResponseWriter, status int, data any, total, limit, offset int) {
	if data != nil {
		rv := reflect.ValueOf(data)
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			data = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(envelope{
		Data: data,
		Meta: &pageMeta{Total: total, Limit: limit, Offset: offset},
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	// Ensure nil slices serialize as [] instead of null.
	if data != nil {
		rv := reflect.ValueOf(data)
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			data = reflect.MakeSlice(rv.Type(), 0, 0).Interface()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(envelope{Data: data})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(envelope{Error: msg})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

// errorStatus maps well-known store errors to HTTP status codes.
func errorStatus(err error) int {
	switch err {
	case store.ErrNotFound:
		return http.StatusNotFound
	case store.ErrConflict:
		return http.StatusConflict
	case store.ErrActiveBranch:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
