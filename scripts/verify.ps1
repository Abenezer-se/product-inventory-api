# Final verification: runs every acceptance check from an EMPTY database.
# WARNING: "docker compose down -v" deletes local data (the seeder restores it).
$ErrorActionPreference = "Continue"
Set-Location (Split-Path -Parent $PSScriptRoot)
$base = "http://localhost:8080"
$dbUrl = "postgres://inventory:inventory_pass@db:5432/inventory?sslmode=disable"
$script:pass = 0
$script:fail = 0

function Section($t) { Write-Host ""; Write-Host "== $t ==" -ForegroundColor Cyan }

function Check($name, [bool]$ok, $detail = "") {
    if ($ok) { Write-Host "[PASS] $name" -ForegroundColor Green; $script:pass++ }
    else { Write-Host "[FAIL] $name $detail" -ForegroundColor Red; $script:fail++ }
}

function Call($method, $path, $body = $null, $ctype = "application/json") {
    $uri = "$base$path"
    try {
        if ($null -ne $body) {
            $r = Invoke-WebRequest -Uri $uri -Method $method -Body $body -ContentType $ctype -UseBasicParsing
        } else {
            $r = Invoke-WebRequest -Uri $uri -Method $method -UseBasicParsing
        }
        return [pscustomobject]@{ Status = [int]$r.StatusCode; Body = [string]$r.Content; Headers = $r.Headers }
    } catch {
        $resp = $_.Exception.Response
        if ($null -eq $resp) { return [pscustomobject]@{ Status = 0; Body = $_.Exception.Message; Headers = $null } }
        $text = ""; $reader = $null
        if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $text = [string]$_.ErrorDetails.Message } else { try { $reader = New-Object System.IO.StreamReader($resp.GetResponseStream()) } catch { $reader = $null } }
        if ($null -ne $reader) { $text = $reader.ReadToEnd() }
        return [pscustomobject]@{ Status = [int]$resp.StatusCode; Body = $text; Headers = $null }
    }
}

function Wait-Healthy {
    for ($i = 0; $i -lt 45; $i++) {
        $r = Call "GET" "/healthz"
        if ($r.Status -eq 200) { return $true }
        Start-Sleep -Seconds 2
    }
    return $false
}

Section "1. Required files"
$files = @("README.md","Makefile","Dockerfile","docker-compose.yml","sqlc.yaml","api.rest",".env.example",".dockerignore",".gitignore","go.mod",
    "migrations\000001_create_products_table.up.sql","migrations\000001_create_products_table.down.sql",
    "internal\products\queries.sql","internal\products\handler_test.go","cmd\api\main.go","cmd\seeder\main.go")
foreach ($f in $files) { Check "file exists: $f" (Test-Path $f) }

$q = Get-Content "internal\products\queries.sql" -Raw
foreach ($n in @("CreateProduct :one","GetProductByID :one","GetProductBySKU :one","ListProducts :many","CountProducts :one","UpdateProduct :one","DeleteProduct :exec")) {
    Check "sqlc query: $n" ($q.Contains("-- name: $n"))
}
$up = Get-Content "migrations\000001_create_products_table.up.sql" -Raw
$down = Get-Content "migrations\000001_create_products_table.down.sql" -Raw
Check "migration: UNIQUE sku" ($up -match "UNIQUE \(sku\)")
Check "migration: CHECK price >= 0" ($up -match "price >= 0")
Check "migration: CHECK stock >= 0" ($up -match "stock_quantity >= 0")
Check "migration: index on status" ($up -match "idx_products_status")
Check "down migration drops table and index" (($down -match "DROP TABLE") -and ($down -match "DROP INDEX"))
Check ".env is git-ignored" ((Get-Content ".gitignore" -Raw) -match "(?m)^\.env\s*$")

Section "2. Code quality"
go vet ./... 2>&1 | Out-Null
Check "go vet" ($LASTEXITCODE -eq 0)
go build ./... 2>&1 | Out-Null
Check "go build" ($LASTEXITCODE -eq 0)
$fmt = (gofmt -l . | Out-String).Trim()
Check "gofmt: all files formatted" ($fmt -eq "") $fmt
docker run --rm -v "${PWD}:/src" -v gomodcache:/go/pkg/mod -w /src golang:1.27-alpine go test ./... 2>&1 | Out-Null
Check "unit tests pass (Linux container)" ($LASTEXITCODE -eq 0)
if (Get-Command make -ErrorAction SilentlyContinue) {
    make -n migrate-up migrate-down seed sqlc build test up docker-seed 2>&1 | Out-Null
    Check "Makefile parses (make -n)" ($LASTEXITCODE -eq 0)
} else {
    Write-Host "[SKIP] make is not installed in this shell" -ForegroundColor Yellow
}
docker run --rm -v "${PWD}:/src" -w /src sqlc/sqlc generate 2>&1 | Out-Null
Check "sqlc generate succeeds" ($LASTEXITCODE -eq 0)
$changed = (git status --porcelain internal/db 2>$null | Out-String).Trim()
Write-Host "       (generated files are reproducible if no unexpected changes appear in internal\db)"

Section "3. Fresh Docker start (docker compose up --build)"
docker compose down -v 2>&1 | Out-Null
$busy = Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue
if ($busy) {
    Write-Host "Port 8080 is in use by another program. Close it (for example a local 'go run') and run this script again." -ForegroundColor Red
    exit 1
}
docker compose up --build -d 2>&1 | Out-Null
Check "docker compose up --build exits OK" ($LASTEXITCODE -eq 0)
$healthy = Wait-Healthy
Check "API answers /healthz" $healthy
if (-not $healthy) {
    docker compose ps -a
    docker compose logs api --tail 20
    Write-Host "API did not become healthy. Send me the lines above." -ForegroundColor Red
    exit 1
}
$h = Call "GET" "/healthz"
Check "healthz reports database up" ($h.Body -match '"database":"up"')
$empty = (Call "GET" "/api/v1/products").Body | ConvertFrom-Json
Check "migration ran automatically (empty list, total_items=0)" ($empty.pagination.total_items -eq 0)

Section "4. Migrations are reversible"
docker compose run --rm migrate -path=/migrations "-database=$dbUrl" down 1 2>&1 | Out-Null
Check "migrate down 1" ($LASTEXITCODE -eq 0)
docker compose run --rm migrate -path=/migrations "-database=$dbUrl" up 2>&1 | Out-Null
Check "migrate up again" ($LASTEXITCODE -eq 0)

Section "5. Database constraints"
$o = (docker exec inventory-db psql -U inventory -d inventory -c "INSERT INTO products (name, sku, price) VALUES ('x','BAD-1',-1);" 2>&1 | ForEach-Object { "$_" }) -join " "
Check "DB rejects negative price" ($o -match "violates check constraint") $o
$o = (docker exec inventory-db psql -U inventory -d inventory -c "INSERT INTO products (name, sku, price, stock_quantity) VALUES ('x','BAD-2',1,-1);" 2>&1 | ForEach-Object { "$_" }) -join " "
Check "DB rejects negative stock" ($o -match "violates check constraint") $o
$o = (docker exec inventory-db psql -U inventory -d inventory -c "INSERT INTO products (name, sku, price, status) VALUES ('x','BAD-3',1,'gone');" 2>&1 | ForEach-Object { "$_" }) -join " "
Check "DB rejects invalid status" ($o -match "violates check constraint") $o

Section "6. Seeder (inside Docker)"
$s1 = (docker compose run --rm api /app/seeder 2>&1 | Out-String)
Check "seeder first run inserts 12" ($s1 -match "inserted=12")
$s2 = (docker compose run --rm api /app/seeder 2>&1 | Out-String)
Check "seeder is idempotent (second run inserts 0)" ($s2 -match "inserted=0")

Section "7. List, pagination and filters"
$j = (Call "GET" "/api/v1/products?page=1&page_size=5").Body | ConvertFrom-Json
Check "pagination: 5 items, total 12, 3 pages" (($j.data.Count -eq 5) -and ($j.pagination.total_items -eq 12) -and ($j.pagination.total_pages -eq 3))
$j = (Call "GET" "/api/v1/products?limit=500").Body | ConvertFrom-Json
Check "page size is capped at 100" ($j.pagination.page_size -eq 100)
$j = (Call "GET" "/api/v1/products?status=active").Body | ConvertFrom-Json
Check "filter status=active -> 7" ($j.pagination.total_items -eq 7)
$j = (Call "GET" "/api/v1/products?status=draft").Body | ConvertFrom-Json
Check "filter status=draft -> 3" ($j.pagination.total_items -eq 3)
$j = (Call "GET" "/api/v1/products?status=archived").Body | ConvertFrom-Json
Check "filter status=archived -> 2" ($j.pagination.total_items -eq 2)
$r = Call "GET" "/api/v1/products?status=bogus"
Check "invalid status filter -> 400" ($r.Status -eq 400)
$j = (Call "GET" "/api/v1/products?q=LAPTOP").Body | ConvertFrom-Json
Check "BONUS search q (case-insensitive) -> 2" ($j.pagination.total_items -eq 2)
$j = (Call "GET" "/api/v1/products?low_stock=true").Body | ConvertFrom-Json
Check "BONUS low_stock=true -> 5" ($j.pagination.total_items -eq 5)
$zero = (Call "GET" "/api/v1/products?page_size=100").Body | ConvertFrom-Json
Check "seed data has a product with stock 0" (@($zero.data | Where-Object { $_.stock_quantity -eq 0 }).Count -ge 1)

Section "8. CRUD endpoints, status codes and error format"
$new1 = '{"name":"Verify Item","sku":"VER-001","description":"check","price":19.99,"stock_quantity":4,"status":"active"}'
$r = Call "POST" "/api/v1/products" $new1
Check "POST create -> 201" ($r.Status -eq 201) "got $($r.Status)"
Check "POST sets Location header" (($null -ne $r.Headers) -and ($r.Headers["Location"] -like "/api/v1/products/*"))
$id1 = ($r.Body | ConvertFrom-Json).id
$r = Call "POST" "/api/v1/products" $new1
Check "POST duplicate SKU -> 409 conflict" (($r.Status -eq 409) -and ($r.Body -match '"error":"conflict"')) "got $($r.Status)"
$r = Call "POST" "/api/v1/products" '{"name":"Second","sku":"VER-002","price":5}'
Check "POST defaults (status draft, stock 0)" (($r.Status -eq 201) -and ($r.Body -match '"status":"draft"') -and ($r.Body -match '"stock_quantity":0'))
$id2 = ($r.Body | ConvertFrom-Json).id
$r = Call "POST" "/api/v1/products" '{"name":"","sku":"x","price":-5,"status":"bad"}'
$e = $null
try { $e = $r.Body | ConvertFrom-Json } catch {}
Check "POST validation -> 400 with field details" (($r.Status -eq 400) -and ($e.error -eq "validation_failed") -and ($null -ne $e.details.name) -and ($null -ne $e.details.price))
$r = Call "POST" "/api/v1/products" '{"name": '
Check "POST invalid JSON -> 400" (($r.Status -eq 400) -and ($r.Body -match '"error":"bad_request"'))
$r = Call "POST" "/api/v1/products" '{"name":"a","sku":"A-1","price":1}' "text/plain"
Check "POST wrong Content-Type -> 415" ($r.Status -eq 415)
$r = Call "GET" "/api/v1/products/$id1"
Check "GET by id -> 200" (($r.Status -eq 200) -and ($r.Body -match "VER-001"))
$r = Call "GET" "/api/v1/products/99999999"
Check "GET missing id -> 404 not_found" (($r.Status -eq 404) -and ($r.Body -match '"error":"not_found"'))
$r = Call "GET" "/api/v1/products/abc"
Check "GET invalid id -> 400" ($r.Status -eq 400)
$r = Call "PATCH" "/api/v1/products/$id1" '{"price":999.5,"stock_quantity":3}'
Check "PATCH update -> 200 with new values" (($r.Status -eq 200) -and ($r.Body -match '"price":999.5') -and ($r.Body -match '"stock_quantity":3'))
$r = Call "PUT" "/api/v1/products/$id1" '{"status":"archived"}'
Check "PUT update -> 200" (($r.Status -eq 200) -and ($r.Body -match '"status":"archived"'))
$r = Call "PUT" "/api/v1/products/$id2" '{"sku":"VER-001"}'
Check "PUT changing SKU to an existing one -> 409" ($r.Status -eq 409) "got $($r.Status)"
$r = Call "PATCH" "/api/v1/products/99999999" '{"price":1}'
Check "PATCH missing id -> 404" ($r.Status -eq 404)
$r = Call "PATCH" "/api/v1/products/$id1" '{"price":-1}'
Check "PATCH negative price -> 400" ($r.Status -eq 400)
$r = Call "DELETE" "/api/v1/products/$id1"
Check "DELETE -> 204" ($r.Status -eq 204)
$r = Call "DELETE" "/api/v1/products/$id1"
Check "DELETE again -> 404" ($r.Status -eq 404)
$r = Call "DELETE" "/api/v1/products/abc"
Check "DELETE invalid id -> 400" ($r.Status -eq 400)
$null = Call "DELETE" "/api/v1/products/$id2"
$r = Call "GET" "/api/v1/nothing-here"
Check "unknown route -> 404 JSON" (($r.Status -eq 404) -and ($r.Body -match '"error":"not_found"'))
$j = (Call "GET" "/api/v1/products").Body | ConvertFrom-Json
Check "test data cleaned up (total back to 12)" ($j.pagination.total_items -eq 12)

Section "9. Persistence and container security"
docker compose down 2>&1 | Out-Null
docker compose up -d 2>&1 | Out-Null
$healthy = Wait-Healthy
Check "API healthy again after down/up" $healthy
$j = (Call "GET" "/api/v1/products").Body | ConvertFrom-Json
Check "data persisted across down/up (total 12)" ($j.pagination.total_items -eq 12)
$idOut = (docker compose exec -T api id 2>&1 | Out-String)
Check "container runs as non-root (uid 10001)" ($idOut -match "uid=10001")
$size = (docker image ls product-inventory-api-api --format "{{.Size}}" | Out-String).Trim()
Write-Host "       image size: $size"

Write-Host ""
Write-Host "PASSED: $script:pass    FAILED: $script:fail"
if ($script:fail -gt 0) {
    Write-Host "SOME CHECKS FAILED. Send me the red [FAIL] lines." -ForegroundColor Red
    exit 1
}
Write-Host "ALL CHECKS PASSED" -ForegroundColor Green
