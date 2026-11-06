package apierror

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Response is the uniform error envelope returned by every failing request.
type Response struct {
	Error   string            `json:"error"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// Write writes an error envelope.
func Write(w http.ResponseWriter, status int, code, message string, details map[string]string) {
	WriteJSON(w, status, Response{Error: code, Message: message, Details: details})
}

func BadRequest(w http.ResponseWriter, message string, details map[string]string) {
	Write(w, http.StatusBadRequest, "bad_request", message, details)
}

func Validation(w http.ResponseWriter, details map[string]string) {
	Write(w, http.StatusBadRequest, "validation_failed", "one or more fields are invalid", details)
}

func NotFound(w http.ResponseWriter, message string) {
	Write(w, http.StatusNotFound, "not_found", message, nil)
}

func Conflict(w http.ResponseWriter, message string) {
	Write(w, http.StatusConflict, "conflict", message, nil)
}

func Internal(w http.ResponseWriter) {
	Write(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred", nil)
}
