.PHONY: build test docs

build:
	go build -trimpath -o terraform-provider-kcore .

test:
	go test ./...

# Regenerates docs/ from provider schema (requires terraform in PATH or downloads it).
docs:
	go tool tfplugindocs generate --rendered-provider-name kcore
