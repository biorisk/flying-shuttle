package api

import (
	"encoding/json"
	"net/http"
	"reflect"
)

// envelope is the JSON response shape for the /api/v1/ingest endpoints.
type envelope struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
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
	_ = json.NewEncoder(w).Encode(envelope{Data: data})
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: msg})
}
