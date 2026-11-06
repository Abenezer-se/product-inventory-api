package products

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"product-inventory-api/internal/db"
)

const (
	DefaultPageSize = 10
	MaxPageSize     = 100

	pgUniqueViolation = "23505"
)

// Store is the data access contract, satisfied by *db.Queries (sqlc).
type Store interface {
	CreateProduct(ctx context.Context, arg db.CreateProductParams) (db.Product, error)
	GetProductByID(ctx context.Context, id int64) (db.Product, error)
	ListProducts(ctx context.Context, arg db.ListProductsParams) ([]db.Product, error)
	CountProducts(ctx context.Context, arg db.CountProductsParams) (int64, error)
	UpdateProduct(ctx context.Context, arg db.UpdateProductParams) (db.Product, error)
	DeleteProduct(ctx context.Context, id int64) error
}

// ListFilter describes pagination and filtering for List.
type ListFilter struct {
	Status   *string
	Search   *string
	LowStock bool
	Page     int
	PageSize int
}

// ListResult is a page of products plus the total number of matches.
type ListResult struct {
	Products []db.Product
	Total    int64
}

// Service holds the business logic for products.
type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, params db.CreateProductParams) (db.Product, error) {
	product, err := s.store.CreateProduct(ctx, params)
	return product, mapError(err)
}

func (s *Service) Get(ctx context.Context, id int64) (db.Product, error) {
	product, err := s.store.GetProductByID(ctx, id)
	return product, mapError(err)
}

func (s *Service) List(ctx context.Context, f ListFilter) (ListResult, error) {
	var lowStock *bool
	if f.LowStock {
		t := true
		lowStock = &t
	}
	offset := (f.Page - 1) * f.PageSize

	items, err := s.store.ListProducts(ctx, db.ListProductsParams{
		Status:    f.Status,
		Search:    f.Search,
		LowStock:  lowStock,
		RowLimit:  int32(f.PageSize),
		RowOffset: int32(offset),
	})
	if err != nil {
		return ListResult{}, mapError(err)
	}

	total, err := s.store.CountProducts(ctx, db.CountProductsParams{
		Status:   f.Status,
		Search:   f.Search,
		LowStock: lowStock,
	})
	if err != nil {
		return ListResult{}, mapError(err)
	}

	return ListResult{Products: items, Total: total}, nil
}

func (s *Service) Update(ctx context.Context, params db.UpdateProductParams) (db.Product, error) {
	product, err := s.store.UpdateProduct(ctx, params)
	return product, mapError(err)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, err := s.store.GetProductByID(ctx, id); err != nil {
		return mapError(err)
	}
	return mapError(s.store.DeleteProduct(ctx, id))
}

// mapError translates database errors into domain errors.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return ErrSKUExists
	}
	return err
}
