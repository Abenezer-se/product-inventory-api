package server

import (
	"mime"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"product-inventory-api/internal/apierror"
)

const requestTimeout = 30 * time.Second

// useMiddleware registers the shared middleware stack. The order matters:
// jsonErrorBodies must come before Recoverer and Timeout so it can see what they write.
func useMiddleware(r chi.Router, timeout time.Duration) {
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(jsonErrorBodies)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(timeout))
	r.Use(requireJSON)
}

// jsonErrorBodies gives the errors produced by chi middleware the same JSON format as the
// rest of the API. Recoverer answers 500 and Timeout answers 504, both with an empty body.
func jsonErrorBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&errorBodyWriter{ResponseWriter: w}, r)
	})
}

type errorBodyWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *errorBodyWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true

	// Our own handlers always set Content-Type before WriteHeader, so a bare
	// 500 or 504 can only come from chi middleware.
	if w.Header().Get("Content-Type") == "" {
		switch status {
		case http.StatusInternalServerError:
			apierror.Internal(w.ResponseWriter)
			return
		case http.StatusGatewayTimeout:
			apierror.Write(w.ResponseWriter, status, "timeout", "the request took too long to process", nil)
			return
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *errorBodyWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the original writer.
func (w *errorBodyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// requireJSON rejects write requests that are not application/json.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				apierror.Write(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
					"Content-Type must be application/json", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
