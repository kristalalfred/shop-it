set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-path := ".data/.env"
export GOCACHE := justfile_directory() / ".cache/go-build"
export GOMODCACHE := justfile_directory() / ".cache/go-mod"

default:
    @just --list

build:
    go build -o bin/ ./cmd/...
    ./bin/shop-it -extension-dir bin/extension

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

run mode="app": build
    case {{quote(mode)}} in \
      app) exec ./bin/shop-it ;; \
      demo) exec ./bin/test-harness -mode demo ;; \
      *) echo "mode must be app or demo" >&2; exit 2 ;; \
    esac

install:
    go install ./cmd/shop-it
    bin="$(go env GOBIN)"; "${bin:-$(go env GOPATH)/bin}/shop-it" -plugin
