set shell := ["sh", "-cu"]

default:
    @just --list

fmt:
    gofmt -w .

fmt-check:
    @files="$(gofmt -l .)"; if [ -n "$files" ]; then printf '%s\n' "$files"; exit 1; fi

tidy:
    go mod tidy

tidy-check:
    go mod tidy -diff

test:
    go test ./...

race:
    go test -race ./...

check: fmt-check tidy-check test

build target="hayate":
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "{{target}}" ./cmd/hayate

run *args:
    go run ./cmd/hayate {{args}}

receive port="50001" output=".":
    go run ./cmd/hayate receive --port "{{port}}" --output "{{output}}"

send file peer:
    go run ./cmd/hayate send "{{file}}" --peer "{{peer}}"

discover duration="5s":
    go run ./cmd/hayate discover --duration "{{duration}}"

release:
    scripts/release.sh

release-race:
    HAYATE_RELEASE_RACE=1 scripts/release.sh

clean:
    rm -rf dist hayate
