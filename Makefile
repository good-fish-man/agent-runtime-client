APP := agent-runtime-client
PKG_LIST := $(shell go list ./... 2>/dev/null)

.PHONY: tidy fmt vet build run test

tidy:
	go mod tidy

fmt:
	gofmt -w .

vet:
	go vet ./...

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o deploy/bin/$(APP) .

run:
	go run . --config manifest/config/config.yaml

test:
	go test -cover ./...
