package products

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"product-inventory-api/internal/apierror"
	"product-inventory-api/internal/db"
)

// ---- In-memory fake of the sqlc store ----

type fakeStore struct {
	mu     sync.Mutex
	items  map[int64]db.Product
	nextID int64
}

var _ Store = (*fakeStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{items: map[int64]db.Product{}, nextID: 1}
}

func (f *fakeStore) skuTaken(sku string, exceptID int64) bool {
	for id, p := range f.items {
		if id != exceptID && p.Sku == sku {
			return true
		}
	}
	return false
}

func (f *fakeStore) filtered(status *string, lowStock *bool) []db.Product {
	out := make([]db.Product, 0, len(f.items))
	for _, p := range f.items {
		if status != nil && p.Status != *status {
			continue
		}
		if lowStock != nil && *lowStock && p.StockQuantity > 5 {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (f *fakeStore) CreateProduct(_ context.Context, arg db.CreateProductParams) (db.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.skuTaken(arg.Sku, 0) {
		return db.Product{}, &pgconn.PgError{Code: "23505"}
	}
	now := time.Now()
	p := db.Product{
		ID:            f.nextID,
		Name:          arg.Name,
		Sku:           arg.Sku,
		Description:   arg.Description,
		Price:         arg.Price,
		StockQuantity: arg.StockQuantity,
		Status:        arg.Status,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	f.items[p.ID] = p
	f.nextID++
	return p, nil
}

func (f *fakeStore) GetProductByID(_ context.Context, id int64) (db.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.items[id]
	if !ok {
		return db.Product{}, pgx.ErrNoRows
	}
	return p, nil
}

func (f *fakeStore) ListProducts(_ context.Context, arg db.ListProductsParams) ([]db.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := f.filtered(arg.Status, arg.LowStock)
	start := int(arg.RowOffset)
	if start > len(items) {
		start = len(items)
	}
	end := start + int(arg.RowLimit)
	if end > len(items) {
		end = len(items)
	}
	return items[start:end], nil
}

func (f *fakeStore) CountProducts(_ context.Context, arg db.CountProductsParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.filtered(arg.Status, arg.LowStock))), nil
}

func (f *fakeStore) UpdateProduct(_ context.Context, arg db.UpdateProductParams) (db.Product, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.items[arg.ID]
	if !ok {
		return db.Product{}, pgx.ErrNoRows
	}
	if arg.Sku != nil {
		if f.skuTaken(*arg.Sku, arg.ID) {
			return db.Product{}, &pgconn.PgError{Code: "23505"}
		}
		p.Sku = *arg.Sku
	}
	if arg.Name != nil {
		p.Name = *arg.Name
	}
	if arg.Description != nil {
		p.Description = arg.Description
	}
	if arg.Price != nil {
		p.Price = *arg.Price
	}
	if arg.StockQuantity != nil {
		p.StockQuantity = *arg.StockQuantity
	}
	if arg.Status != nil {
		p.Status = *arg.Status
	}
	p.UpdatedAt = time.Now()
	f.items[p.ID] = p
	return p, nil
}

func (f *fakeStore) DeleteProduct(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, id)
	return nil
}

// seed inserts n products: SKU-001.., even ids active, odd ids draft, stock equals the number.
func (f *fakeStore) seed(t *testing.T, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		status := StatusDraft
		if i%2 == 0 {
			status = StatusActive
		}
		_, err := f.CreateProduct(context.Background(), db.CreateProductParams{
			Name:          fmt.Sprintf("Product %d", i),
			Sku:           fmt.Sprintf("SKU-%03d", i),
			Price:         float64(i),
			StockQuantity: int32(i),
			Status:        status,
		})
		if err != nil {
			t.Fatalf("seed failed: %v", err)
		}
	}
}

// ---- Helpers ----

func newTestServer() (http.Handler, *fakeStore) {
	store := newFakeStore()
	r := chi.NewRouter()
	r.Mount("/api/v1/products", NewHandler(NewService(store)).Routes())
	return r, store
}

func doRequest(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("invalid JSON response %q: %v", rec.Body.String(), err)
	}
	return v
}

// ---- Create ----

func TestCreateProduct(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantError  string
		wantField  string
	}{
		{"valid minimal", `{"name":"Mouse","sku":"MSE-001","price":9.99}`, 201, "", ""},
		{"valid maximum price", `{"name":"Gold","sku":"GLD-001","price":99999999.99}`, 201, "", ""},
		{"empty name", `{"name":"  ","sku":"MSE-001","price":9.99}`, 400, "validation_failed", "name"},
		{"missing sku", `{"name":"Mouse","price":9.99}`, 400, "validation_failed", "sku"},
		{"lowercase sku", `{"name":"Mouse","sku":"mse-001","price":9.99}`, 400, "validation_failed", "sku"},
		{"missing price", `{"name":"Mouse","sku":"MSE-001"}`, 400, "validation_failed", "price"},
		{"negative price", `{"name":"Mouse","sku":"MSE-001","price":-1}`, 400, "validation_failed", "price"},
		{"three decimals", `{"name":"Mouse","sku":"MSE-001","price":1.999}`, 400, "validation_failed", "price"},
		{"price too large", `{"name":"Mouse","sku":"MSE-001","price":100000000}`, 400, "validation_failed", "price"},
		{"negative stock", `{"name":"Mouse","sku":"MSE-001","price":1,"stock_quantity":-5}`, 400, "validation_failed", "stock_quantity"},
		{"invalid status", `{"name":"Mouse","sku":"MSE-001","price":1,"status":"deleted"}`, 400, "validation_failed", "status"},
		{"wrong type", `{"name":"Mouse","sku":"MSE-001","price":"cheap"}`, 400, "validation_failed", "price"},
		{"invalid json", `{"name": `, 400, "bad_request", ""},
		{"empty body", ``, 400, "bad_request", ""},
		{"unknown field", `{"name":"Mouse","sku":"MSE-001","price":1,"color":"red"}`, 400, "bad_request", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newTestServer()
			rec := doRequest(t, h, http.MethodPost, "/api/v1/products", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantError == "" {
				return
			}
			body := decode[apierror.Response](t, rec)
			if body.Error != tt.wantError {
				t.Errorf("error code = %q, want %q", body.Error, tt.wantError)
			}
			if tt.wantField != "" {
				if _, ok := body.Details[tt.wantField]; !ok {
					t.Errorf("details %v is missing field %q", body.Details, tt.wantField)
				}
			}
		})
	}
}

func TestCreateProduct_Defaults(t *testing.T) {
	h, _ := newTestServer()
	rec := doRequest(t, h, http.MethodPost, "/api/v1/products", `{"name":"Mouse","sku":"MSE-001","price":9.99}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/products/1" {
		t.Errorf("Location = %q", loc)
	}
	p := decode[db.Product](t, rec)
	if p.Status != StatusDraft || p.StockQuantity != 0 || p.Description != nil {
		t.Errorf("unexpected defaults: %+v", p)
	}
}

func TestCreateProduct_DuplicateSKU(t *testing.T) {
	h, _ := newTestServer()
	body := `{"name":"Mouse","sku":"MSE-001","price":9.99}`
	if rec := doRequest(t, h, http.MethodPost, "/api/v1/products", body); rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d", rec.Code)
	}
	rec := doRequest(t, h, http.MethodPost, "/api/v1/products", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if got := decode[apierror.Response](t, rec).Error; got != "conflict" {
		t.Errorf("error code = %q", got)
	}
}

// ---- Get ----

func TestGetProduct(t *testing.T) {
	h, store := newTestServer()
	store.seed(t, 3)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantError  string
	}{
		{"found", "/api/v1/products/1", 200, ""},
		{"not found", "/api/v1/products/999", 404, "not_found"},
		{"text id", "/api/v1/products/abc", 400, "bad_request"},
		{"zero id", "/api/v1/products/0", 400, "bad_request"},
		{"negative id", "/api/v1/products/-1", 400, "bad_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, h, http.MethodGet, tt.path, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantError != "" {
				if got := decode[apierror.Response](t, rec).Error; got != tt.wantError {
					t.Errorf("error code = %q, want %q", got, tt.wantError)
				}
			}
		})
	}
}

// ---- List ----

func TestListProducts(t *testing.T) {
	h, store := newTestServer()
	store.seed(t, 25)

	tests := []struct {
		name      string
		query     string
		wantItems int
		wantTotal int64
		wantPages int64
		wantPage  int
		wantSize  int
	}{
		{"defaults", "", 10, 25, 3, 1, 10},
		{"last page", "?page=3", 5, 25, 3, 3, 10},
		{"custom size", "?page=2&page_size=20", 5, 25, 2, 2, 20},
		{"limit alias", "?limit=5", 5, 25, 5, 1, 5},
		{"size capped at 100", "?page_size=500", 25, 25, 1, 1, 100},
		{"status filter", "?status=active", 10, 12, 2, 1, 10},
		{"low stock filter", "?low_stock=true", 5, 5, 1, 1, 10},
		{"page beyond end", "?page=9", 0, 25, 3, 9, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, h, http.MethodGet, "/api/v1/products"+tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
			}
			body := decode[listResponse](t, rec)
			if body.Data == nil {
				t.Fatal("data must be an empty array, not null")
			}
			if len(body.Data) != tt.wantItems {
				t.Errorf("items = %d, want %d", len(body.Data), tt.wantItems)
			}
			pg := body.Pagination
			if pg.TotalItems != tt.wantTotal || pg.TotalPages != tt.wantPages ||
				pg.Page != tt.wantPage || pg.PageSize != tt.wantSize {
				t.Errorf("pagination = %+v", pg)
			}
		})
	}
}

func TestListProducts_InvalidQuery(t *testing.T) {
	h, _ := newTestServer()
	for _, q := range []string{"?status=bogus", "?page=0", "?page=abc", "?page_size=0", "?low_stock=maybe"} {
		t.Run(q, func(t *testing.T) {
			rec := doRequest(t, h, http.MethodGet, "/api/v1/products"+q, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if got := decode[apierror.Response](t, rec).Error; got != "validation_failed" {
				t.Errorf("error code = %q", got)
			}
		})
	}
}

// ---- Update ----

func TestUpdateProduct(t *testing.T) {
	h, store := newTestServer()
	store.seed(t, 2)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantError  string
	}{
		{"patch price and stock", http.MethodPatch, "/api/v1/products/1", `{"price":49.5,"stock_quantity":7}`, 200, ""},
		{"put name and status", http.MethodPut, "/api/v1/products/1", `{"name":"Renamed","status":"archived"}`, 200, ""},
		{"not found", http.MethodPatch, "/api/v1/products/999", `{"price":1}`, 404, "not_found"},
		{"invalid id", http.MethodPatch, "/api/v1/products/abc", `{"price":1}`, 400, "bad_request"},
		{"empty update", http.MethodPatch, "/api/v1/products/1", `{}`, 400, "validation_failed"},
		{"negative price", http.MethodPatch, "/api/v1/products/1", `{"price":-1}`, 400, "validation_failed"},
		{"invalid status", http.MethodPatch, "/api/v1/products/1", `{"status":"gone"}`, 400, "validation_failed"},
		{"sku conflict", http.MethodPatch, "/api/v1/products/2", `{"sku":"SKU-001"}`, 409, "conflict"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, h, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantError != "" {
				if got := decode[apierror.Response](t, rec).Error; got != tt.wantError {
					t.Errorf("error code = %q, want %q", got, tt.wantError)
				}
			}
		})
	}
}

func TestUpdateProduct_ChangesOnlyGivenFields(t *testing.T) {
	h, store := newTestServer()
	store.seed(t, 1)

	rec := doRequest(t, h, http.MethodPatch, "/api/v1/products/1", `{"price":49.5,"stock_quantity":7}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	p := decode[db.Product](t, rec)
	if p.Price != 49.5 || p.StockQuantity != 7 {
		t.Errorf("price/stock not updated: %+v", p)
	}
	if p.Name != "Product 1" || p.Sku != "SKU-001" {
		t.Errorf("untouched fields changed: %+v", p)
	}
}

// ---- Delete ----

func TestDeleteProduct(t *testing.T) {
	h, store := newTestServer()
	store.seed(t, 1)

	if rec := doRequest(t, h, http.MethodDelete, "/api/v1/products/abc", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid id status = %d, want 400", rec.Code)
	}
	rec := doRequest(t, h, http.MethodDelete, "/api/v1/products/1", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 response must have an empty body, got %q", rec.Body.String())
	}
	if rec := doRequest(t, h, http.MethodDelete, "/api/v1/products/1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", rec.Code)
	}
}

// ---- Validation helpers ----

func TestCheckPrice(t *testing.T) {
	tests := []struct {
		name   string
		in     float64
		wantOK bool
		want   float64
	}{
		{"zero", 0, true, 0},
		{"one decimal", 5.5, true, 5.5},
		{"two decimals", 10.99, true, 10.99},
		{"maximum", 99999999.99, true, 99999999.99},
		{"negative", -0.01, false, 0},
		{"three decimals", 10.999, false, 0},
		{"too large", 100000000, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, msg := checkPrice(tt.in)
			if (msg == "") != tt.wantOK {
				t.Fatalf("checkPrice(%v) message = %q, wantOK = %v", tt.in, msg, tt.wantOK)
			}
			if tt.wantOK && got != tt.want {
				t.Errorf("checkPrice(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
