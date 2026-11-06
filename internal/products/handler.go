package products

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"product-inventory-api/internal/apierror"
	"product-inventory-api/internal/db"
)

const maxBodyBytes = 1 << 20 // 1 MB

// Handler exposes the products HTTP endpoints.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Routes returns the router mounted at /api/v1/products.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", h.Create)
	r.Get("/", h.List)
	r.Get("/{id}", h.Get)
	r.Put("/{id}", h.Update)
	r.Patch("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	return r
}

type pagination struct {
	TotalItems int64 `json:"total_items"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalPages int64 `json:"total_pages"`
}

type listResponse struct {
	Data       []db.Product `json:"data"`
	Pagination pagination   `json:"pagination"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateProductRequest
	if !decodeBody(w, r, &req) {
		return
	}
	params, details := req.Validate()
	if len(details) > 0 {
		apierror.Validation(w, details)
		return
	}
	product, err := h.svc.Create(r.Context(), params)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/products/"+strconv.FormatInt(product.ID, 10))
	apierror.WriteJSON(w, http.StatusCreated, product)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	filter, details := parseListQuery(r)
	if len(details) > 0 {
		apierror.Validation(w, details)
		return
	}
	result, err := h.svc.List(r.Context(), filter)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	var totalPages int64
	if result.Total > 0 {
		size := int64(filter.PageSize)
		totalPages = (result.Total + size - 1) / size
	}
	apierror.WriteJSON(w, http.StatusOK, listResponse{
		Data: result.Products,
		Pagination: pagination{
			TotalItems: result.Total,
			Page:       filter.Page,
			PageSize:   filter.PageSize,
			TotalPages: totalPages,
		},
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	product, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	apierror.WriteJSON(w, http.StatusOK, product)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var req UpdateProductRequest
	if !decodeBody(w, r, &req) {
		return
	}
	params, details := req.Validate()
	if len(details) > 0 {
		apierror.Validation(w, details)
		return
	}
	params.ID = id
	product, err := h.svc.Update(r.Context(), params)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	apierror.WriteJSON(w, http.StatusOK, product)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		apierror.BadRequest(w, "id must be a positive integer", map[string]string{"id": "invalid id format"})
		return 0, false
	}
	return id, true
}

func parseListQuery(r *http.Request) (ListFilter, map[string]string) {
	q := r.URL.Query()
	details := map[string]string{}
	filter := ListFilter{Page: 1, PageSize: DefaultPageSize}

	if v := q.Get("page"); v != "" {
		page, err := strconv.Atoi(v)
		if err != nil || page < 1 {
			details["page"] = "page must be an integer greater than or equal to 1"
		} else {
			filter.Page = page
		}
	}

	sizeKey := "page_size"
	if q.Get(sizeKey) == "" {
		sizeKey = "limit"
	}
	if v := q.Get(sizeKey); v != "" {
		size, err := strconv.Atoi(v)
		switch {
		case err != nil || size < 1:
			details[sizeKey] = sizeKey + " must be an integer greater than or equal to 1"
		case size > MaxPageSize:
			filter.PageSize = MaxPageSize
		default:
			filter.PageSize = size
		}
	}

	if v := q.Get("status"); v != "" {
		if !isValidStatus(v) {
			details["status"] = "status must be one of: draft, active, archived"
		} else {
			status := v
			filter.Status = &status
		}
	}

	search := q.Get("q")
	if search == "" {
		search = q.Get("search")
	}
	if search = strings.TrimSpace(search); search != "" {
		escaped := escapeLike(search)
		filter.Search = &escaped
	}

	if v := q.Get("low_stock"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			details["low_stock"] = "low_stock must be true or false"
		} else {
			filter.LowStock = b
		}
	}

	if len(details) == 0 && int64(filter.Page-1)*int64(filter.PageSize) > math.MaxInt32 {
		details["page"] = "page is too large"
	}

	return filter, details
}

// escapeLike makes user input match literally inside an ILIKE pattern.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		writeDecodeError(w, err)
		return false
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		apierror.BadRequest(w, "request body must contain a single JSON object", nil)
		return false
	}
	return true
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	var tooLarge *http.MaxBytesError

	switch {
	case errors.As(err, &tooLarge):
		apierror.Write(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body must not exceed 1 MB", nil)
	case errors.Is(err, io.EOF):
		apierror.BadRequest(w, "request body must not be empty", nil)
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		apierror.BadRequest(w, "request body contains invalid JSON", nil)
	case errors.As(err, &typeErr):
		field := typeErr.Field
		if field == "" {
			field = "body"
		}
		apierror.Write(w, http.StatusBadRequest, "validation_failed",
			"request body contains a value of the wrong type",
			map[string]string{field: "must be " + jsonTypeName(typeErr.Type)})
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		apierror.BadRequest(w, "request body contains an unknown field: "+
			strings.TrimPrefix(err.Error(), "json: unknown field "), nil)
	default:
		apierror.BadRequest(w, "request body contains invalid JSON", nil)
	}
}

func jsonTypeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Int, reflect.Int32, reflect.Int64:
		return "an integer"
	default:
		return fmt.Sprintf("of type %s", t.String())
	}
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		apierror.NotFound(w, "product not found")
	case errors.Is(err, ErrSKUExists):
		apierror.Conflict(w, "a product with this sku already exists")
	default:
		slog.Error("request failed", "error", err, "request_id", middleware.GetReqID(r.Context()))
		apierror.Internal(w)
	}
}
