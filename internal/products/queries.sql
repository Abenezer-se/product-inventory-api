-- name: CreateProduct :one
INSERT INTO products (name, sku, description, price, stock_quantity, status)
VALUES (
    sqlc.arg('name'),
    sqlc.arg('sku'),
    sqlc.narg('description'),
    sqlc.arg('price'),
    sqlc.arg('stock_quantity'),
    sqlc.arg('status')
)
RETURNING *;

-- name: GetProductByID :one
SELECT * FROM products
WHERE id = sqlc.arg('id');

-- name: GetProductBySKU :one
SELECT * FROM products
WHERE sku = sqlc.arg('sku');

-- name: ListProducts :many
SELECT * FROM products
WHERE (sqlc.narg('status')::varchar IS NULL OR status = sqlc.narg('status')::varchar)
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('low_stock')::boolean IS NOT TRUE OR stock_quantity <= 5)
ORDER BY id
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CountProducts :one
SELECT COUNT(*) FROM products
WHERE (sqlc.narg('status')::varchar IS NULL OR status = sqlc.narg('status')::varchar)
  AND (sqlc.narg('search')::text IS NULL OR name ILIKE '%' || sqlc.narg('search')::text || '%')
  AND (sqlc.narg('low_stock')::boolean IS NOT TRUE OR stock_quantity <= 5);

-- name: UpdateProduct :one
UPDATE products SET
    name           = COALESCE(sqlc.narg('name'), name),
    sku            = COALESCE(sqlc.narg('sku'), sku),
    description    = COALESCE(sqlc.narg('description'), description),
    price          = COALESCE(sqlc.narg('price'), price),
    stock_quantity = COALESCE(sqlc.narg('stock_quantity'), stock_quantity),
    status         = COALESCE(sqlc.narg('status'), status),
    updated_at     = NOW()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteProduct :exec
DELETE FROM products
WHERE id = sqlc.arg('id');
