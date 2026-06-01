.PHONY: test lint lint-fix

test:
	go test ./... -race -count=1

lint:
	golangci-lint run ./...

lint-fix:
	golangci-lint run ./... --fix
