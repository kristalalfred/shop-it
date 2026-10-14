set shell := ["bash", "-euo", "pipefail", "-c"]
export GOCACHE := justfile_directory() / ".cache/go-build"

default:
    @just --list

build:
    go build -o bin/ship-it ./cmd/ship-it
    go build -o bin/mockio ./cmd/mockio

check:
    test -z "$(gofmt -l cmd internal tests)"
    go vet ./...
    go test -race ./...

run config="config/dev.json": build
    ./bin/ship-it -config {{quote(config)}}

demo: build
    #!/usr/bin/env bash
    set -euo pipefail
    task_pids=()
    cleanup() {
      for task_pid in "${task_pids[@]}"; do kill "$task_pid" 2>/dev/null || true; done
      for task_pid in "${task_pids[@]}"; do wait "$task_pid" 2>/dev/null || true; done
    }
    trap cleanup EXIT
    trap 'exit 0' INT TERM
    ./bin/mockio &
    task_pids+=("$!")
    ./bin/ship-it &
    task_pids+=("$!")
    for attempt in {1..50}; do
      if curl --fail --silent http://127.0.0.1:18080/healthz >/dev/null; then break; fi
      sleep 0.1
    done
    curl --fail --silent http://127.0.0.1:18080/healthz
    ./bin/mockio -mode browser &
    task_pids+=("$!")
    while true; do
      for task_pid in "${task_pids[@]}"; do kill -0 "$task_pid" 2>/dev/null || exit 1; done
      sleep 1
    done

request action="search":
    #!/usr/bin/env bash
    set -euo pipefail
    task_action={{quote(action)}}
    case "$task_action" in
      search) task_endpoint=catalog/search ;;
      transfer) task_endpoint=basket-transfers ;;
      *) exit 2 ;;
    esac
    curl --fail-with-body --silent --show-error \
      -H 'Authorization: Bearer local-demo-api-token' \
      -H 'Content-Type: application/json' \
      -H 'Idempotency-Key: demo-transfer' \
      --data-binary "@testdata/$task_action.json" \
      "http://127.0.0.1:18080/v1/$task_endpoint"
