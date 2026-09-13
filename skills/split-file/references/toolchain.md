# Toolchain for splitting Go files

`scripts/preflight.sh` checks everything and builds the helper binaries into
`${CLAUDE_PLUGIN_DATA}/bin` (falls back to `scripts/bin`).

| Tool | Role | Install | Fallback |
|---|---|---|---|
| **go** (required) | Compiler is the primary gate: unused imports, undefined names, and import cycles are all build errors. | go.dev/dl | none |
| **movedecl** (built) | Deterministic move of top-level decls (with doc comments) between files; runs goimports. | built by preflight from `scripts/gotools` | gopls `refactor.extract.toNewFile` code action (interactive IDE-oriented; harder to drive from CLI) |
| **snapshot** (built) | Per-declaration hash oracle; detects MISSING/ADDED/CHANGED/MOVED across the whole module. | built by preflight | `git diff --stat` + reviewer (weaker) |
| **inventory** (built) | Map of a large file: decls, methods per type, package state, `init()`, clusters. | built by preflight | `grep -n "^func \|^type "` |
| **gopls** (required) | `gopls rename` for exporting names during sub-package extraction; `gopls references` for impact. Serena MCP uses it under the hood for Go. | `go install golang.org/x/tools/gopls@latest` | none for cross-package renames; do not sed-rename |
| **goimports** (required) | Fix imports after moves. | `go install golang.org/x/tools/cmd/goimports@latest` | `go build` errors + manual import edits |
| **golangci-lint** (optional) | `funlen`, `gocyclo`, `gocognit`, `nestif`, `maintidx`, `depguard` in the gate and CI. | `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` | `go vet` only |
| **deadcode** (optional) | Delete unreachable code before splitting. | `go install golang.org/x/tools/cmd/deadcode@latest` | `golangci-lint` `unused` |
| **gofumpt** (optional) | Stricter formatting; use if the repo already does. | `go install mvdan.cc/gofumpt@latest` | gofmt |

## Building the helper tools by hand

```bash
cd "$CLAUDE_PLUGIN_ROOT/skills/split-file/scripts/gotools" && go build -o "$CLAUDE_PLUGIN_DATA/bin/" ./...
```
If the build fails, report the compiler output; do not work around a broken helper
by moving code manually.

## Serena MCP
If configured, extractors may use Serena's `find_referencing_symbols` for impact and
`rename` for exports. Keep movedecl for the move itself so one cluster has one mechanism.
