.PHONY: build test bench run lint dev backup migrate

build:
	go build -o bin/server ./cmd/server

test:
	@go list ./... | grep -v '/web/node_modules/' | xargs go test -race -count=1 -v

bench:
	go test ./internal/... -bench=. -benchmem -benchtime=5s

run:
	go run ./cmd/server

lint:
	@go list ./... | grep -v '/web/node_modules/' | xargs go vet

dev:
	@echo "Starting backend and frontend..."
	@go run ./cmd/server &
	@cd web && npm run dev

backup:
	go run ./cmd/admin backup --db $${DB_PATH:-data/cache-engine.db} --out $${BACKUP_OUT:-backups/cache-engine.sqlite3}

migrate:
	go run ./cmd/admin migrate --db $${DB_PATH:-data/cache-engine.db}
