.PHONY: build web check test dev
build: web
	mkdir -p bin
	go build -o bin/teamcross ./cmd/teamcross
web:
	pnpm --filter @teamcross/web build
check:
	go vet ./...
	pnpm --filter @teamcross/web check
test:
	go test ./...
	pnpm --filter @teamcross/web test
dev:
	go run ./cmd/teamcross serve --dev-web http://127.0.0.1:5173
