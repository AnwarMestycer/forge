# forge task runner — install with: sudo dnf install just

# Container engine for the testbed; override with FORGE_COMPOSE="podman compose".
compose := env("FORGE_COMPOSE", "docker compose")

default:
    @just --list

build:
    go build -o bin/forge ./cmd/forge
    cd proxy && cargo build --release

test:
    go test ./...
    cd proxy && cargo test

lint:
    golangci-lint run
    cd proxy && cargo clippy -- -D warnings

# Local release dry run (both binaries, tarballs, rpm/deb) into dist/
snapshot:
    goreleaser release --snapshot --clean

run-testbed:
    {{compose}} -f testbed/docker-compose.yml up -d --build

stop-testbed:
    {{compose}} -f testbed/docker-compose.yml down
