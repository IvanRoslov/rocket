package api

import (
	"encoding/json"
	"net/http"
)

// errBody is the JSON shape for error responses: {"error":{"code":"...","message":"..."}}.
type errBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeErr writes a JSON error response with the given HTTP status, machine
// code, and human-readable message.
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	var body errBody
	body.Error.Code = code
	body.Error.Message = msg
	writeJSON(w, status, body)
}

// writeJSON writes v as a JSON response body with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
