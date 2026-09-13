# refactor-go

A Claude Code plugin for safely decomposing oversized Go files in agent-written
codebases, and for keeping them small afterwards.

**Key idea for Go:** files in one package share a namespace, so splitting a god-file into
several same-package files changes no imports, visibility, or call sites. That is phase
one and usually the only phase. Sub-package extraction is a separate, optional phase that
the compiler gates (import cycles are build errors) and that must never be "fixed" with
interfaces or a `common` package.

**What it enforces:** relocations only through `movedecl` (deterministic, keeps doc
comments, runs goimports); every step passes a gate (gofmt, build, vet, golangci-lint,
per-declaration hash oracle, tests) before commit; extraction fans out to parallel
subagents in git worktrees and fans back in one merge at a time; a headless Codex (GPT-5.6 Sol, ultra) review followed by a Fable 5.1 review that adjudicates
both the diff and Codex's findings, with a consensus round on disagreements; a hook and CI stop files from regrowing.

## Install

```bash
claude --plugin-dir ./refactor-go                  # development
# or project-scoped, auto-loads next session:
mkdir -p .claude/skills && cp -r refactor-go .claude/skills/refactor-go
```

In the repo (module root): `bash "$CLAUDE_PLUGIN_ROOT/skills/split-file/scripts/preflight.sh" --install`
This builds the helper binaries (`inventory`, `movedecl`, `snapshot`) from
`scripts/gotools` into `$CLAUDE_PLUGIN_DATA/bin` and installs gopls/goimports if missing.

Add to `.claude/settings.json`: `{ "worktree": { "baseRef": "head" } }`.

## Use

```
/refactor-go:split-file pkg/engine/engine.go
/refactor-go:split-file pkg/engine/engine.go --to-package     # also extract approved sub-packages
/refactor-go:guardrails --max-lines 500
/refactor-go:split-file-workflow   with args {"files": ["pkg/engine/engine.go", "pkg/api/server.go"]}
```

## Layout

```
refactor-go/
├── .claude-plugin/plugin.json
├── skills/
│   ├── split-file/
│   │   ├── SKILL.md
│   │   ├── references/           # toolchain, playbook, model-roles
│   │   └── scripts/
│   │       ├── preflight.sh, verify.sh, check_file_length.sh
│   │       └── gotools/          # go module: inventory/, movedecl/, snapshot/, internal/decls
│   └── guardrails/               # golangci, pre-commit/make, CI, CLAUDE.md, hook templates
├── agents/                       # split-planner (Fable 5.1), plan-critic (Opus 5),
│                                 # extractor (Opus 4.8, worktree), gate-runner (Opus 4.8),
│                                 # refactor-reviewer (Fable 5.1)
├── workflows/split-file.js
└── hooks/hooks.json              # PostToolUse file-size guard
```

## Helper tools (stdlib-only Go)

- `inventory -file big.go [-json out.json]`: decls, methods per type, package-level
  state, `init()`, clusters, leaves.
- `movedecl -file src.go -dest dst.go -names A,T,T.M [-apply] [-package name]`: moves
  decls with doc comments; grouped `var (...)` blocks move whole; dry run by default.
- `snapshot -out before.json ./...` / `snapshot -compare before.json ./... [-strict]`:
  reports MISSING / ADDED / CHANGED / MOVED (cross-package relocation recognized by hash).

The tools were written without a Go toolchain available in the authoring environment;
`preflight.sh` compiles them on first use and reports compiler output if anything needs
fixing. They use only the standard library (`go/ast`, `go/parser`, `go/printer`, `go/format`).

## Second-opinion review (Codex)

Phase 6a runs `scripts/codex_review.sh`, which follows the operational playbook of the
[codex-orchestrator](https://github.com/alexzh3/codex-orchestrator) plugin (install it too;
its skill is the reference for locating the binary, run modes, and resume/consensus).
Requires Codex CLI or the IDE extension, signed in. `ultra` needs the ChatGPT backend; on
API-key providers the script falls back to `xhigh`.

## Requirements
Go ≥ 1.21, git, gopls, goimports (required); golangci-lint, deadcode, gofumpt (recommended).

## Validate
```bash
claude plugin validate ./refactor-go --strict
```
