set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-path := ".data/.env"
export GOCACHE := justfile_directory() / ".cache/go-build"
export GOMODCACHE := justfile_directory() / ".cache/go-mod"

default:
    @just --list

build:
    go build -o bin/ ./cmd/...
    ./bin/ship-it -extension-dir bin/extension

test case="all": build
    if [ {{quote(case)}} = all ]; then node --test extension/tests/*.test.mjs tests/web/*.test.mjs; fi
    ./bin/test-harness -case {{quote(case)}}

check: build
    test -z "$(gofmt -l cmd internal tests extension plugin)"
    go vet ./...
    for script in internal/backend/web/app.js internal/backend/plugin/app.js extension/assets/*.js; do node --check "$script"; done
    node --test extension/tests/*.test.mjs tests/web/*.test.mjs
    hurlfmt --check tests/api/*.hurl
    ./bin/test-harness

run mode="backend" config="": build
    #!/usr/bin/env bash
    set -euo pipefail
    SHIP_IT_CONFIG={{quote(config)}}
    case {{quote(mode)}} in
      demo) exec ./bin/test-harness -mode demo -config "${SHIP_IT_CONFIG:-config/dev.json}" ;;
      backend) exec ./bin/ship-it -config "${SHIP_IT_CONFIG:-.data/local.json}" ;;
      *) exit 2 ;;
    esac

plugin config=".data/local.json" listen="127.0.0.1:18081": build
    ./bin/ship-it -config {{quote(config)}} -listen {{quote(listen)}} -plugin-dir bin/ship-it-plugin
