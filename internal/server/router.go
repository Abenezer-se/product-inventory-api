package server

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"product-inventory-api/internal/apierror"
	"product-inventory-api/internal/db"
	"product-inventory-api/internal/products"
)

// NewRouter wires middleware, routes and dependencies together.
func NewRouter(pool *pgxpool.Pool) http.Handler {
	queries := db.New(pool)
	productService := products.NewService(queries)
	productHandler := products.NewHandler(productService)

	r := chi.NewRouter()
	useMiddleware(r, requestTimeout)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		apierror.NotFound(w, "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		apierror.Write(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this route", nil)
	})

	r.Get("/healthz", healthHandler(pool))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
			apierror.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		})
		r.Mount("/products", productHandler.Routes())
	})

	return r
}

// healthHandler reports whether the API can reach the database.
func healthHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			apierror.WriteJSON(w, http.StatusServiceUnavailable,
				map[string]string{"status": "unavailable", "database": "down"})
			return
		}
		apierror.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "up"})
	}
}
