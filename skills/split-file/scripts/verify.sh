#!/usr/bin/env bash
# The gate. Run after every extraction. Exit 0 = safe to commit.
#   verify.sh [--pkg ./path/...] [--snapshot before.json] [--strict-bodies] [--fast]
# Env: REFACTOR_TEST_CMD (default "go test ./..."), REFACTOR_MAX_LINES (500)
set -u
HERE=$(cd "$(dirname "$0")" && pwd); BIN=${CLAUDE_PLUGIN_DATA:-$HERE}/bin; [ -x "$BIN/snapshot" ] || BIN=$HERE/bin
PKG=./...; SNAP=""; STRICT=""; FAST=0
while [ $# -gt 0 ]; do case "$1" in
  --pkg) PKG=$2; shift 2;; --snapshot) SNAP=$2; shift 2;; --strict-bodies) STRICT="-strict"; shift;; --fast) FAST=1; shift;; *) echo "unknown arg $1"; exit 2;; esac; done
LOG=.refactor-gate.log; : > "$LOG"; fail=0; results=()
run() { local label=$1; shift; echo "--- $label: $*" >> "$LOG"; if "$@" >> "$LOG" 2>&1; then results+=("PASS $label"); else results+=("FAIL $label"); fail=1; fi; }

run "gofmt"        bash -c 'test -z "$(gofmt -l $(git ls-files "*.go" | grep -v vendor/) 2>&1)"'
run "build"        go build ./...
run "vet"          go vet ./...
run "file-length"  bash "$HERE/check_file_length.sh" .
if command -v golangci-lint >/dev/null 2>&1 && ls .golangci.y*ml >/dev/null 2>&1; then run "golangci-lint" golangci-lint run ./...; else results+=("SKIP golangci-lint (not installed or no config)"); fi
if [ -n "$SNAP" ]; then run "bodies-unchanged" "$BIN/snapshot" -compare "$SNAP" $STRICT .; else results+=("SKIP bodies-unchanged (no --snapshot)"); fi
if [ $FAST -eq 0 ]; then run "tests" bash -c "${REFACTOR_TEST_CMD:-go test ./...}"; else results+=("SKIP tests (--fast)"); fi

echo "== gate results =="; printf '%s\n' "${results[@]}"
if [ $fail -ne 0 ]; then
  first=$(printf '%s\n' "${results[@]}" | grep '^FAIL' | head -1 | awk '{print $2}')
  echo "== first failure detail ($first, tail of $LOG) =="
  awk -v lbl="--- $first:" 'index($0,lbl)==1{p=1;next} /^--- /{if(p)exit} p' "$LOG" | tail -60
  echo "GATE: FAIL"; exit 1
fi
echo "GATE: PASS"
