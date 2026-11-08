package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"product-inventory-api/internal/apierror"
)

func newTestRouter(timeout time.Duration) *chi.Mux {
	r := chi.NewRouter()
	useMiddleware(r, timeout)
	return r
}

func serve(h http.Handler, method, path, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(""))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) apierror.Response {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var resp apierror.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("body is not valid JSON: %q (%v)", rec.Body.String(), err)
	}
	return resp
}

func TestPanicReturnsJSONError(t *testing.T) {
	r := newTestRouter(time.Second)
	r.Get("/panic", func(w http.ResponseWriter, r *http.Request) { panic("boom") })

	rec := serve(r, http.MethodGet, "/panic", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := decodeError(t, rec).Error; got != "internal_error" {
		t.Errorf("error code = %q, want internal_error", got)
	}
}

func TestTimeoutReturnsJSONError(t *testing.T) {
	r := newTestRouter(20 * time.Millisecond)
	r.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})

	rec := serve(r, http.MethodGet, "/slow", "")
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
	if got := decodeError(t, rec).Error; got != "timeout" {
		t.Errorf("error code = %q, want timeout", got)
	}
}

func TestHandlerResponsesAreNotRewritten(t *testing.T) {
	r := newTestRouter(time.Second)
	r.Get("/custom", func(w http.ResponseWriter, r *http.Request) {
		apierror.Write(w, http.StatusInternalServerError, "custom_code", "custom message", nil)
	})
	r.Get("/created", func(w http.ResponseWriter, r *http.Request) {
		apierror.WriteJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
	})
	r.Delete("/gone", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	rec := serve(r, http.MethodGet, "/custom", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("custom status = %d, want 500", rec.Code)
	}
	if got := decodeError(t, rec).Error; got != "custom_code" {
		t.Errorf("handler error was rewritten: %q", got)
	}

	rec = serve(r, http.MethodGet, "/created", "")
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Errorf("created response changed: %d %q", rec.Code, rec.Body.String())
	}

	rec = serve(r, http.MethodDelete, "/gone", "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("204 response changed: %d %q", rec.Code, rec.Body.String())
	}
}

func TestRequireJSON(t *testing.T) {
	r := newTestRouter(time.Second)
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	r.Post("/items", ok)
	r.Get("/items", ok)

	tests := []struct {
		name        string
		method      string
		contentType string
		want        int
	}{
		{"post json", http.MethodPost, "application/json", http.StatusNoContent},
		{"post json with charset", http.MethodPost, "application/json; charset=utf-8", http.StatusNoContent},
		{"post text", http.MethodPost, "text/plain", http.StatusUnsupportedMediaType},
		{"post without content type", http.MethodPost, "", http.StatusUnsupportedMediaType},
		{"get needs no content type", http.MethodGet, "", http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(r, tt.method, "/items", tt.contentType)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if tt.want == http.StatusUnsupportedMediaType {
				if got := decodeError(t, rec).Error; got != "unsupported_media_type" {
					t.Errorf("error code = %q", got)
				}
			}
		})
	}
}
