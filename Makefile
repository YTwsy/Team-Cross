.PHONY: build check dev fmt test web bridge

build: web bridge
	mkdir -p bin
	go build -o bin/teamcross ./cmd/teamcross

web:
	pnpm --filter @teamcross/web build

bridge:
	pnpm --filter @teamcross/agent-bridge build

check:
	go vet ./...
	pnpm check

test:
	go test ./...
	pnpm test

dev:
	go run ./cmd/teamcross serve --repo . --dev-web http://127.0.0.1:5173

fmt:
	gofmt -w cmd internal
	pnpm -r format
