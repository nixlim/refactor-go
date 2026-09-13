---
name: guardrails
description: >-
  Install and verify file-size, complexity, and package-boundary guardrails for a Go repo
  worked on by AI agents: golangci-lint rules (funlen, gocyclo, gocognit, nestif, maintidx,
  depguard), a max-code-lines pre-commit hook with a grandfather baseline, a CI job, and a
  CLAUDE.md/AGENTS.md section pointing at the enforcing tools. Use after a file split, when
  files keep growing, when setting up a new agent-driven Go repo, or when asked for
  "guardrails", "lint rules for file size", "stop files from getting huge", or "enforce
  package boundaries".
argument-hint: "[--max-lines N]"
allowed-tools: Bash(bash *), Bash(go *), Bash(git *), Bash(golangci-lint *), Bash(pre-commit *), Read, Write, Edit, Grep, Glob
---

# Guardrails for agent-written Go

Prose rules get partial compliance; `go vet`, golangci-lint, and a size hook that fails the
build get near-total compliance. Install the tools, then make the prose point at them.

Templates: `${CLAUDE_PLUGIN_ROOT}/skills/guardrails/templates/`. Size checker:
`${CLAUDE_PLUGIN_ROOT}/skills/split-file/scripts/check_file_length.sh`. Default budget:
500 code lines per file.

## Steps
1. **Detect existing config**: `.golangci.yml`/`.golangci.yaml`, `.pre-commit-config.yaml`,
   `Makefile` targets, CI workflows, `CLAUDE.md`, `AGENTS.md`. Merge; never overwrite.
2. **Vendor the checker**: copy `check_file_length.sh` to `scripts/check_file_length.sh`,
   `chmod +x`, so CI and pre-commit do not depend on the plugin.
3. **Baseline**: `bash scripts/check_file_length.sh --write-baseline .refactor-baseline.tsv --max <N> .`
   Show the grandfathered list; those are `/refactor-go:split-file` candidates.
4. **golangci-lint**: merge `templates/golangci.yml` (linters: `funlen`, `gocyclo`,
   `gocognit`, `nestif`, `maintidx`, `depguard`, `unused`, `govet`, `errcheck`, `staticcheck`).
   Keep thresholds; if the repo has many existing violations use `issues.new-from-rev`
   so only new code is enforced, and record the rev in a comment.
5. **Pre-commit / make**: merge `templates/pre-commit-config.yaml` (gofmt, go vet, size
   check, golangci-lint) or add a `make guardrails` target from `templates/Makefile.snippet`
   if the repo does not use pre-commit.
6. **Package boundaries**: fill `depguard` rules in `.golangci.yml` from
   `templates/golangci.yml` (which packages may import which). For layered repos add
   `go-arch-lint` or `arch-go` only if the team already uses one.
7. **CI**: merge `templates/ci-guardrails.yml`; the job fails on vet, lint, size.
8. **Agent instructions**: append `templates/CLAUDE.md.snippet` to `CLAUDE.md` (and
   `AGENTS.md` if present); near the top if the file is long.
9. **Hook**: the plugin's PostToolUse guard is active when the plugin is enabled; for
   plugin-less repos merge `templates/settings.hooks.json` into `.claude/settings.json`.
10. **Verify**: create a temporary 600-line `.go` file, confirm the hook message, confirm
    pre-commit/CI command fail, delete it. Report what was installed.

## Thresholds
| Rule | Default | Why |
|---|---|---|
| file code lines | 500 | one Read call with headroom; parallel agents rarely collide |
| funlen | 60 lines / 40 statements | golangci default is 60/40 |
| gocyclo | 15 | golangci default |
| gocognit | 20 | |
| nestif | 5 | |
Raise a threshold only with a comment in `.golangci.yml` explaining why.
