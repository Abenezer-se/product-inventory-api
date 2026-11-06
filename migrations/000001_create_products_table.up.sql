-- Products table for the Product Inventory System.
-- The UNIQUE constraint on sku automatically creates a unique index on sku.
CREATE TABLE products (
    id             BIGSERIAL      PRIMARY KEY,
    name           VARCHAR(255)   NOT NULL,
    sku            VARCHAR(64)    NOT NULL,
    description    TEXT,
    price          NUMERIC(10, 2) NOT NULL,
    stock_quantity INTEGER        NOT NULL DEFAULT 0,
    status         VARCHAR(32)    NOT NULL DEFAULT 'draft',
    created_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ    NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_products_sku UNIQUE (sku),
    CONSTRAINT chk_products_name_not_empty CHECK (btrim(name) <> ''),
    CONSTRAINT chk_products_sku_format CHECK (sku ~ '^[A-Z0-9][A-Z0-9-]*$'),
    CONSTRAINT chk_products_price_non_negative CHECK (price >= 0.00),
    CONSTRAINT chk_products_stock_non_negative CHECK (stock_quantity >= 0),
    CONSTRAINT chk_products_status_valid CHECK (status IN ('draft', 'active', 'archived'))
);

CREATE INDEX idx_products_status ON products (status);
