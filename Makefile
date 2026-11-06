.PHONY: run build test fmt sqlc migrate-up migrate-down seed up down logs docker-seed

# Database URL as seen from inside the Docker network (service name "db").
DOCKER_DB_URL ?= postgres://inventory:inventory_pass@db:5432/inventory?sslmode=disable
MIGRATE = docker compose run --rm migrate -path=/migrations -database="$(DOCKER_DB_URL)"

# ---- Local development ----
run:
	go run ./cmd/api

build:
	go build -ldflags="-s -w" -o bin/api ./cmd/api

test:
	go test ./...

fmt:
	go fmt ./... && go vet ./...

# ---- Code generation ----
sqlc:
	docker run --rm -v "$(CURDIR):/src" -w /src sqlc/sqlc generate

# ---- Database ----
migrate-up:
	$(MIGRATE) up

migrate-down:
	$(MIGRATE) down 1

seed:
	go run ./cmd/seeder

# ---- Docker ----
up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f api

docker-seed:
	docker compose run --rm api /app/seeder
