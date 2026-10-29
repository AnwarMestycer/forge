# forge task runner — install with: sudo dnf install just

default:
    @just --list

build:
    go build -o bin/forge ./cmd/forge
    cd proxy && cargo build --release

test:
    go test ./...
    cd proxy && cargo test

lint:
    go vet ./...
    cd proxy && cargo clippy -- -D warnings

run-testbed:
    docker compose -f testbed/docker-compose.yml up -d
