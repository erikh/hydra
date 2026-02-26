.PHONY: test lint

test: lint
	go test ./... -count=1

lint:
	go vet ./...
	golangci-lint run ./...
