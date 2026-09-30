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
	go run ./cmd/teamcross serve --foreground --dev-web http://127.0.0.1:5173

.PHONY: desktop-build desktop-test
desktop-build:
	python3 scripts/build-desktop-app.py
desktop-test:
	cd apps/desktop && go test -race ./internal/...

.PHONY: release verify-release verify-homebrew verify-app-instance verify-desktop-bundle
VERSION ?= 0.2.6-dev
release:
	python3 scripts/build-release.py --version $(VERSION)
verify-release:
	python3 scripts/verify-release.py dist/release/$(VERSION)
verify-homebrew:
	python3 scripts/verify-homebrew.py dist/release/$(VERSION)
verify-desktop-bundle:
	python3 scripts/verify-desktop-bundle.py dist/release/$(VERSION)
# Swift reference only. Wails native acceptance is in the installation test gates.
verify-app-instance:
	python3 scripts/verify-app-instance.py 'dist/release/$(VERSION)/Team Cross.app'
