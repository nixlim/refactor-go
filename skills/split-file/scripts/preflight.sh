#!/usr/bin/env bash
# Preflight for refactor-go: verify/install the toolchain, build the Go helper tools, check repo state.
#   preflight.sh            report (exit 1 if a REQUIRED tool is missing or tools fail to build)
#   preflight.sh --install  also `go install` missing Go tools
set -u
HERE=$(cd "$(dirname "$0")" && pwd); INSTALL=0; [ "${1:-}" = "--install" ] && INSTALL=1
BIN=${CLAUDE_PLUGIN_DATA:-$HERE}/bin; mkdir -p "$BIN" 2>/dev/null || BIN=$HERE/bin
ok=(); missing=(); opt=()
have() { command -v "$1" >/dev/null 2>&1; }
gi() { echo "installing $2 ..." >&2; GOBIN="$(go env GOPATH)/bin" go install "$2" >/dev/null 2>&1; export PATH="$PATH:$(go env GOPATH)/bin"; }
check() { local name=$1 req=$2 pkg=${3:-}
  if ! have "$name" && [ $INSTALL -eq 1 ] && [ -n "$pkg" ]; then gi "$name" "$pkg"; fi
  if have "$name"; then ok+=("$name"); elif [ "$req" = 1 ]; then missing+=("$name"); else opt+=("$name"); fi; }

echo "== toolchain =="
check git 1; check go 1
if have go; then
  check gopls 1 golang.org/x/tools/gopls@latest                       # rename, references, extract-to-new-file
  check goimports 1 golang.org/x/tools/cmd/goimports@latest          # import fixing after moves
  check golangci-lint 0 github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
  check deadcode 0 golang.org/x/tools/cmd/deadcode@latest
  check gofumpt 0 mvdan.cc/gofumpt@latest
  echo "== building helper tools into $BIN =="
  if (cd "$HERE/gotools" && go build -o "$BIN/" ./... 2>&1); then ok+=("gotools"); else missing+=("gotools-build"); fi
fi
echo "present : ${ok[*]:-none}"; echo "missing : ${missing[*]:-none}"; echo "optional: ${opt[*]:-none}"; echo "BIN=$BIN"

echo "== codex (second-opinion review) =="
CODEX=$(command -v codex 2>/dev/null); [ -z "$CODEX" ] && CODEX=$(find ~/.cursor/extensions ~/.vscode/extensions ~/.vscode-server/extensions -maxdepth 4 -name codex -type f 2>/dev/null | head -1)
if [ -n "$CODEX" ]; then echo "codex=$CODEX ($("$CODEX" --version 2>/dev/null | head -1))"; else echo "WARN: codex not found; Phase 6a (GPT-5.6 Sol review) will be skipped. Install Codex CLI or the IDE extension, and the codex-orchestrator plugin."; fi
echo "== repo state =="
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  branch=$(git rev-parse --abbrev-ref HEAD); dirty=$(git status --porcelain | wc -l | tr -d ' ')
  echo "branch=$branch dirty_files=$dirty"
  [ "$dirty" != 0 ] && echo "WARN: dirty tree; commit or stash so every step is revertible"
  [ -f go.mod ] || echo "WARN: no go.mod at repo root; run from the module root"
else echo "ERROR: not a git repository"; missing+=("git-repo"); fi
echo "== claude code settings =="
if grep -qs '"baseRef"' .claude/settings.json .claude/settings.local.json 2>/dev/null && grep -qs '"head"' .claude/settings.json .claude/settings.local.json 2>/dev/null; then echo "worktree.baseRef=head found"
else echo "NOTE: set {\"worktree\": {\"baseRef\": \"head\"}} in .claude/settings.json so subagent worktrees branch from THIS branch"; fi
echo "== summary =="
[ ${#missing[@]} -gt 0 ] && { echo "PREFLIGHT FAILED: ${missing[*]} (re-run with --install)"; exit 1; }
echo "PREFLIGHT OK"
