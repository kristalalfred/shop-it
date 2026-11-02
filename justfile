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

package:
    test -z "$(git status --porcelain)" || { echo "commit before packaging so release.json names the source" >&2; exit 1; }
    rm -rf bin/dist && mkdir -p bin/dist
    revision="$(git rev-parse HEAD)"; \
    for pair in aarch64-apple-darwin:darwin/arm64 x86_64-apple-darwin:darwin/amd64 aarch64-unknown-linux-gnu:linux/arm64 x86_64-unknown-linux-gnu:linux/amd64; do \
      target="${pair%%:*}" platform="${pair#*:}" stage="$(mktemp -d)"; \
      mkdir "$stage/bin"; \
      CGO_ENABLED=0 GOOS="${platform%/*}" GOARCH="${platform#*/}" go build -trimpath -ldflags=-s -o "$stage/bin/shop-it" ./cmd/shop-it; \
      printf '{\n  "project": "shop-it",\n  "target": "%s",\n  "source_revision": "%s"\n}\n' "$target" "$revision" > "$stage/release.json"; \
      tar -czf "bin/dist/shop-it-$target.tar.gz" -C "$stage" bin release.json; \
      (cd bin/dist && shasum -a 256 "shop-it-$target.tar.gz" > "shop-it-$target.tar.gz.sha256"); \
      rm -rf "$stage"; \
    done

publish-firefox: build
    WEB_EXT_API_KEY="$AMO_JWT_ISSUER" WEB_EXT_API_SECRET="$AMO_JWT_SECRET" pnpm dlx web-ext@10 sign --source-dir bin/extension/firefox --artifacts-dir bin/web-ext-artifacts --channel listed --amo-metadata extension/store/amo-metadata.json --approval-timeout 0
