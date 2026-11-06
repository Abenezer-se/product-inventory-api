package products

import "errors"

var (
	// ErrNotFound is returned when a product does not exist.
	ErrNotFound = errors.New("product not found")
	// ErrSKUExists is returned when a SKU violates the unique constraint.
	ErrSKUExists = errors.New("sku already exists")
)
