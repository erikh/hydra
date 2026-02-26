.PHONY: test lint install

test: lint
	go test ./... -count=1

install:
	go install -v ./...

lint:
	go vet ./...
	golangci-lint run ./...
