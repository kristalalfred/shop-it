set shell := ["bash", "-euo", "pipefail", "-c"]
export GOCACHE := justfile_directory() / ".cache/go-build"
export GOMODCACHE := justfile_directory() / ".cache/go-mod"

default:
    @just --list

build:
    go build -o bin/ ./cmd/...

test case="all": build
    ./bin/test-harness -case {{quote(case)}}

check:
    test -z "$(gofmt -l cmd internal tests)"
    go vet ./...
    node --check internal/backend/web/app.js
    hurlfmt --check tests/api/*.hurl
    just test

run mode="demo" config="config/dev.json": build
    #!/usr/bin/env bash
    set -euo pipefail
    case {{quote(mode)}} in
      demo) exec ./bin/test-harness -mode demo -config {{quote(config)}} ;;
      backend) exec ./bin/ship-it -config {{quote(config)}} ;;
      *) exit 2 ;;
    esac
