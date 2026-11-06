package products

import (
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"product-inventory-api/internal/db"
)

const (
	StatusDraft    = "draft"
	StatusActive   = "active"
	StatusArchived = "archived"

	maxNameLength        = 255
	maxSKULength         = 64
	maxDescriptionLength = 5000
	maxPrice             = 99999999.99
)

var skuPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]*$`)

// CreateProductRequest is the JSON body of POST /api/v1/products.
type CreateProductRequest struct {
	Name          string   `json:"name"`
	SKU           string   `json:"sku"`
	Description   *string  `json:"description"`
	Price         *float64 `json:"price"`
	StockQuantity *int64   `json:"stock_quantity"`
	Status        *string  `json:"status"`
}

// UpdateProductRequest is the JSON body of PUT/PATCH /api/v1/products/{id}.
// Only the fields that are present are changed.
type UpdateProductRequest struct {
	Name          *string  `json:"name"`
	SKU           *string  `json:"sku"`
	Description   *string  `json:"description"`
	Price         *float64 `json:"price"`
	StockQuantity *int64   `json:"stock_quantity"`
	Status        *string  `json:"status"`
}

func isValidStatus(s string) bool {
	switch s {
	case StatusDraft, StatusActive, StatusArchived:
		return true
	}
	return false
}

func checkName(name string) (string, string) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", "name must not be empty"
	case utf8.RuneCountInString(name) > maxNameLength:
		return "", "name must be at most 255 characters"
	}
	return name, ""
}

func checkSKU(sku string) string {
	switch {
	case sku == "":
		return "sku is required"
	case len(sku) > maxSKULength:
		return "sku must be at most 64 characters"
	case !skuPattern.MatchString(sku):
		return "sku must contain only uppercase letters, digits and hyphens, and start with a letter or digit"
	}
	return ""
}

func checkDescription(d string) string {
	if utf8.RuneCountInString(d) > maxDescriptionLength {
		return "description must be at most 5000 characters"
	}
	return ""
}

func checkPrice(p float64) (float64, string) {
	if math.IsNaN(p) || math.IsInf(p, 0) {
		return 0, "price must be a valid number"
	}
	if p < 0 {
		return 0, "price must be greater than or equal to 0"
	}
	if p > maxPrice {
		return 0, "price must not exceed 99999999.99"
	}
	if decimalPlaces(p) > 2 {
		return 0, "price must have at most 2 decimal places"
	}
	return math.Round(p*100) / 100, ""
}

func checkStock(q int64) string {
	switch {
	case q < 0:
		return "stock_quantity must be greater than or equal to 0"
	case q > math.MaxInt32:
		return "stock_quantity is too large"
	}
	return ""
}

func checkStatus(s string) string {
	if !isValidStatus(s) {
		return "status must be one of: draft, active, archived"
	}
	return ""
}

// Validate checks the request and returns sqlc parameters, or field errors.
func (r CreateProductRequest) Validate() (db.CreateProductParams, map[string]string) {
	details := map[string]string{}
	params := db.CreateProductParams{Status: StatusDraft}

	name, msg := checkName(r.Name)
	if msg != "" {
		details["name"] = msg
	}
	params.Name = name

	if msg := checkSKU(r.SKU); msg != "" {
		details["sku"] = msg
	}
	params.Sku = r.SKU

	if r.Description != nil {
		if msg := checkDescription(*r.Description); msg != "" {
			details["description"] = msg
		}
		params.Description = r.Description
	}

	if r.Price == nil {
		details["price"] = "price is required"
	} else if price, msg := checkPrice(*r.Price); msg != "" {
		details["price"] = msg
	} else {
		params.Price = price
	}

	if r.StockQuantity != nil {
		if msg := checkStock(*r.StockQuantity); msg != "" {
			details["stock_quantity"] = msg
		} else {
			params.StockQuantity = int32(*r.StockQuantity)
		}
	}

	if r.Status != nil {
		if msg := checkStatus(*r.Status); msg != "" {
			details["status"] = msg
		} else {
			params.Status = *r.Status
		}
	}

	return params, details
}

// Validate checks the request and returns sqlc parameters, or field errors.
func (r UpdateProductRequest) Validate() (db.UpdateProductParams, map[string]string) {
	details := map[string]string{}
	params := db.UpdateProductParams{}

	if r.Name == nil && r.SKU == nil && r.Description == nil && r.Price == nil &&
		r.StockQuantity == nil && r.Status == nil {
		details["body"] = "at least one field must be provided"
		return params, details
	}

	if r.Name != nil {
		name, msg := checkName(*r.Name)
		if msg != "" {
			details["name"] = msg
		} else {
			params.Name = &name
		}
	}

	if r.SKU != nil {
		if msg := checkSKU(*r.SKU); msg != "" {
			details["sku"] = msg
		} else {
			params.Sku = r.SKU
		}
	}

	if r.Description != nil {
		if msg := checkDescription(*r.Description); msg != "" {
			details["description"] = msg
		} else {
			params.Description = r.Description
		}
	}

	if r.Price != nil {
		if price, msg := checkPrice(*r.Price); msg != "" {
			details["price"] = msg
		} else {
			params.Price = &price
		}
	}

	if r.StockQuantity != nil {
		if msg := checkStock(*r.StockQuantity); msg != "" {
			details["stock_quantity"] = msg
		} else {
			q := int32(*r.StockQuantity)
			params.StockQuantity = &q
		}
	}

	if r.Status != nil {
		if msg := checkStatus(*r.Status); msg != "" {
			details["status"] = msg
		} else {
			params.Status = r.Status
		}
	}

	return params, details
}
