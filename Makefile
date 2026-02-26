.PHONY: test lint install snapshot release tag full-release

VERSION ?= v$(shell date +%Y.%m.%d)

test: lint
	go test ./... -count=1

install:
	go install -v ./...

lint:
	go vet ./...
	golangci-lint run ./...

snapshot:
	goreleaser build --snapshot --clean

release:
	goreleaser release --clean

tag:
	git tag -s -m "Release $(VERSION)" $(VERSION)
	git push origin $(VERSION)

full-release: tag release
