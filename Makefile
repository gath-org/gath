.PHONY: build test lint devnet

build:
	go build -o bin/gath ./cmd/gath

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l .)"

devnet: build
	bash scripts/devnet.sh
