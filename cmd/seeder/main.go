package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"product-inventory-api/internal/config"
)

type seedProduct struct {
	Name        string
	SKU         string
	Description string
	Price       float64
	Stock       int32
	Status      string
}

var seedData = []seedProduct{
	{"Laptop Pro 15", "LAP-001", "15 inch laptop with 16 GB RAM and 512 GB SSD", 1299.99, 25, "active"},
	{"Ultrabook Air 13", "LAP-002", "Lightweight 13 inch ultrabook, 12 hour battery", 999.00, 14, "active"},
	{"Wireless Mouse M200", "ACC-001", "Ergonomic wireless mouse with USB receiver", 24.50, 150, "active"},
	{"Mechanical Keyboard K7", "ACC-002", "Backlit mechanical keyboard, brown switches", 89.99, 60, "active"},
	{"USB-C Hub 7-in-1", "ACC-003", "HDMI, USB 3.0, SD card reader and power delivery", 45.00, 3, "active"},
	{"27 inch 4K Monitor", "MON-001", "IPS display, 60 Hz, adjustable stand", 429.00, 8, "active"},
	{"Noise Cancelling Headphones", "AUD-001", "Bluetooth over-ear headphones, 30 hour battery", 199.99, 0, "active"},
	{"Portable SSD 1TB", "STO-001", "USB 3.2 portable solid state drive", 119.90, 40, "draft"},
	{"Webcam HD 1080p", "CAM-001", "Full HD webcam with built-in microphone", 59.00, 0, "draft"},
	{"Smartphone Stand", "ACC-004", "", 12.75, 200, "draft"},
	{"Laptop Pro 13 (2022)", "LAP-900", "Previous generation model, no longer sold", 899.00, 5, "archived"},
	{"Wired Earbuds E10", "AUD-900", "Discontinued wired earbuds", 9.99, 2, "archived"},
}

const insertSQL = `
INSERT INTO products (name, sku, description, price, stock_quantity, status)
VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6)
ON CONFLICT (sku) DO NOTHING`

func main() {
	if err := run(); err != nil {
		slog.Error("seeding failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DBURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var inserted int64
	for _, p := range seedData {
		tag, err := tx.Exec(ctx, insertSQL, p.Name, p.SKU, p.Description, p.Price, p.Stock, p.Status)
		if err != nil {
			return err
		}
		inserted += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	slog.Info("seeding complete",
		"inserted", inserted,
		"skipped_existing", int64(len(seedData))-inserted)
	return nil
}
