.PHONY: build test lint install

build:
	go build ./cmd/ctxed

test:
	go test -race ./...

lint:
	test -z "$$(gofmt -l .)"
	go vet ./...
	~/go/bin/staticcheck ./...

install:
	go install ./cmd/ctxed
