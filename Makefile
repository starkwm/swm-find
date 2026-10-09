.DEFAULT_GOAL := build
.PHONY: build test check fmt

build:
	go build -trimpath -o bin/swm-find ./cmd/swm-find
	go build -trimpath -o bin/swm-find-eval ./cmd/swm-find-eval

test:
	go test -race ./...

check:
	@test -z "$$(gofmt -l $$(find cmd internal -name '*.go'))"
	go vet ./...
	go test -race ./...

fmt:
	gofmt -w cmd internal
