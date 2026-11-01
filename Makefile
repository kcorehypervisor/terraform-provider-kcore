.PHONY: build test lint vuln ci docs

build:
	go build -trimpath -o terraform-provider-kcore .

test:
	go test ./...

# The same checks as the lint job in .github/workflows/ci.yml.
lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go mod tidy -diff
	go vet ./...
	golangci-lint run ./...
	terraform fmt -check -recursive examples

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

# Everything the pull request checks run, except CodeQL and the GoReleaser snapshot build.
ci: lint
	go test -race -count=1 ./...
	$(MAKE) vuln

# Regenerates docs/ from provider schema (requires terraform in PATH or downloads it).
docs:
	go tool tfplugindocs generate --rendered-provider-name kcore
