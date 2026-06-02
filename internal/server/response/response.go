package response

import (
	"encoding/json"
	"net/http"
)

// Error matches the OpenAPI Error schema: {code, message, details}.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// JSON writes v as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// Fail writes a structured error envelope.
func Fail(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, Error{Code: code, Message: message})
}
