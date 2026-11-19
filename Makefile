.PHONY: web test build check

web:
	cd web && npm ci && npm run build

test:
	go test -race ./...
	cd web && npm test

build:
	go build -o bin/realm ./cmd/realm

check: test
	gofmt -l . | (! grep .)
	go vet ./...
