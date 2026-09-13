---
name: split-file
description: >-
  Safely decompose an oversized Go file (anything over ~500 code lines, especially
  multi-thousand-line "god files") into several files of the same package and, only when
  justified, into sub-packages. Uses deterministic tools (movedecl for relocation, a
  per-declaration hash oracle, go build/vet/test as the gate) and parallel subagents in git
  worktrees. Use whenever a Go file is too large, blocks parallel agent work, trips the
  file-size guard, or someone says "split", "break up", "decompose", "this file is huge",
  or "extract into packages". Use proactively before adding to a file already over budget.
argument-hint: "<path/to/file.go> [--max-lines N] [--to-package]"
allowed-tools: Bash(bash *), Bash(go *), Bash(git *), Bash(gopls *), Bash(goimports *), Bash(golangci-lint *), Read, Grep, Glob, Agent
---

# Split a Go god-file

You are the **orchestrator**: plan, delegate, gate, merge. You never cut-and-paste code
yourself; relocations go through `movedecl` and every step must pass `verify.sh` before
it is committed. Freehand LLM moves drop helpers and edit bodies silently; a tool plus a
hash oracle cannot.

```bash
S="${CLAUDE_PLUGIN_ROOT}/skills/split-file/scripts"
BIN="${CLAUDE_PLUGIN_DATA:-$S}/bin"        # helper binaries built by preflight
```

Target: `$ARGUMENTS`. If no file was given, run `bash $S/check_file_length.sh .` and ask
which file to split.

## Go changes the job (read this first)

Files in one package share a namespace. **Moving a declaration to another file of the
same package changes no imports, no visibility, no call sites.** That makes the first
and usually only phase cheap: same-package file split. Only extract a *sub-package*
(second phase, `--to-package`) when a cluster has a clean exported surface and no
dependency back on the parent package; a sub-package that needs the parent creates an
import cycle, which the compiler rejects and which must NOT be "fixed" with interfaces.

## Phase 0: Preflight

1. `bash $S/preflight.sh` (builds the helper tools into `$BIN`); on missing REQUIRED
   tools run `bash $S/preflight.sh --install` and re-run. Stop on PREFLIGHT FAILED.
2. Feature branch (`git switch -c refactor/split-<name>`), clean tree, `go build ./... &&
   go test ./...` green. A red baseline makes the gate meaningless: stop and report.
3. `.claude/settings.json` must contain `"worktree": {"baseRef": "head"}` (preflight
   prints a NOTE otherwise); without it extractor worktrees branch from the default branch.

## Phase 1: Freeze behavior

```bash
mkdir -p .refactor
$BIN/snapshot -out .refactor/before.json ./...
bash $S/check_file_length.sh --write-baseline .refactor-baseline.tsv .
git add .refactor .refactor-baseline.tsv && git commit -m "refactor: freeze baseline for split of <file>"
```

If `go test -cover ./<pkg>/` shows the file's exported functions are barely covered,
first delegate to a `general-purpose` subagent to write characterization tests
(table-driven, pinning current outputs, no production edits) and commit them.

## Phase 2: Plan, then critique

1. `$BIN/inventory -file <file> -json .refactor/inventory.json > .refactor/inventory.md`
2. Delegate to **`refactor-go:split-planner`** (Fable 5.1) with the inventory paths. It
   returns `.refactor/plan-<name>.md`: target files (same package), each type with all
   its methods together, `init()` and package-level state staying in one owning file,
   and extraction **waves** (clusters with disjoint decls that can run in parallel).
   Sub-package candidates are listed separately with their exported surface.
3. Delegate to **`refactor-go:plan-critic`** (Opus 5). One revision round; if still
   blocked, show the user both documents and stop.
4. Print the plan summary; in an interactive session wait for approval.

## Phase 3: Fan-out (same-package file split, parallel in worktrees)

For each wave, spawn one **`refactor-go:extractor`** (Opus 4.8, `isolation: worktree`)
per cluster in the same turn. Brief, verbatim shape:

```
Cluster: <name>
Source file: <dir>/<big>.go
Destination file: <dir>/<target>.go
Decls to move, in this order: <A, T, T.Method, B>
Snapshot: .refactor/before.json
Scripts: <S>   Binaries: <BIN>
Rules: move only with movedecl; never edit bodies; never add interfaces or packages;
commit on your worktree branch only when verify.sh passes; return the JSON report.
```

Cap concurrency at min(clusters, 8). Each extractor returns
`{"cluster","branch","commit","gate":"pass|fail","moved":[...],"notes"}`.

Escalation per cluster: resume with gate output → fresh extractor on `claude-opus-5` →
planner re-plans smaller → stop and report. Do not skip rungs.

## Phase 4: Fan-in (one merge at a time, re-gate, never hand-resolve)

Merge branches in plan order; after each: `bash $S/verify.sh --snapshot .refactor/before.json`
(delegate to **`refactor-go:gate-runner`** to keep output out of your context). On a
conflict: `git merge --abort`, spawn a new extractor for that cluster on the updated
HEAD, merge its result. Commit each wave: `refactor(<pkg>): split <big>.go (wave N)`.

Because the split is same-package, no re-exports are needed. `go build` proves it.

## Phase 5 (optional, `--to-package`): extract a sub-package

Only for clusters the plan marked as sub-package candidates. Sequential, one at a time:
1. Extractor uses `movedecl -dest <dir>/<sub>/<file>.go -package <sub>` for the cluster.
2. Unexported names now used across the boundary must be exported: `gopls rename` on
   each (deterministic, updates all references). List every rename in `notes`.
3. Callers in the parent import the sub-package (`goimports` handles it once the names
   exist). If the sub-package needs anything from the parent, that is an import cycle:
   **stop and report**; do not introduce interfaces, callbacks, or a `common` package.
4. Gate, commit `refactor(<pkg>): extract <sub> package`.

## Phase 6: Two-model review (Codex first, then Fable adjudicates)

Self-review by the model family that did the work reproduces its blind spots, so the
review is heterogeneous and sequential:

**6a. Headless Codex review (GPT-5.6 Sol, reasoning effort `ultra`).**

```bash
bash $S/codex_review.sh --base <baseline-commit> --plan .refactor/plan-<name>.md --out .refactor/codex-review.md
```

The script encodes the operational details from the `codex-orchestrator` plugin (which
should be installed; `/codex-orchestrator:codex-orchestrator` is the reference if the
script needs adapting): locates the IDE-bundled binary when `codex` is not on PATH, runs
`codex exec` in `-s read-only -c approval_policy=never`, captures the final message with
`-o`, keeps the session so it can be resumed, and falls back to `xhigh` if the backend
rejects `ultra` (API-key providers do; ChatGPT-signed-in Codex accepts it). Do not read
the JSONL log; read only `.refactor/codex-review.md`. If Codex is unavailable, record
`CODEX: unavailable (<reason>)` in the final report and continue; do not skip 6b.

**6b. Fable adjudicates both the changes and Codex's verdict.** Delegate to
**`refactor-go:refactor-reviewer`** (Fable 5.1; deliberately not the extractor's
model) with: baseline commit, HEAD, the plan, `.refactor/before.json`, `$S`, `$BIN`, and
`.refactor/codex-review.md`. It (1) checks the diff itself with the oracle and targeted
reads, (2) verifies every Codex BLOCKING finding against the code and marks it
CONFIRMED / REFUTED (with evidence) / UNVERIFIABLE, (3) adds findings Codex missed, and
(4) returns one consolidated verdict plus `ALLOWED_CHANGED`.

**6c. Consensus round (only on disagreement).** For each Codex finding the reviewer
REFUTED, run one round: `bash $S/codex_review.sh --followup <codex_thread> "<finding + reviewer's evidence>"`
(the thread id is at the bottom of `codex-review.md`). Codex answers AGREE / DISAGREE /
RETRACT with evidence. Hand that answer back to the same reviewer (resume it). A finding
both models still disagree on after one round is escalated to the user with both sides'
evidence; it is never silently dropped. Record every disagreement and its resolution in
`.refactor/consensus-<name>.md` (finding, Codex evidence, Fable evidence, outcome).

Fix blocking findings through the extractor → gate → merge loop, then re-run 6a and 6b on
the new HEAD (Codex resumes its thread, so the second pass is cheap).

## Phase 7: Finalize

1. `bash $S/check_file_length.sh --write-baseline .refactor-baseline.tsv .` then
   `bash $S/check_file_length.sh .` must pass for the split package.
2. `bash $S/verify.sh --snapshot .refactor/before.json --strict-bodies` (use
   `-allow-changed` via the reviewer's ALLOWED_CHANGED list if any body legitimately changed).
3. If a sub-package was extracted, add a `depguard`/`gomodguard` rule (guardrails skill)
   so the parent cannot start importing it circularly through a third package.
4. Report: new files with code-line counts, decls moved, gate status, Codex verdict, Fable
   verdict, consensus record summary.

## Rules that override everything above

- Never edit a function or method body during a split; never rename during a
  same-package split; never add interfaces, base types, or packages to make something
  compile. Report the cycle instead.
- Never merge a branch whose gate did not pass. One cluster per commit.
- Never `Read` the god-file whole: use `.refactor/inventory.md`, `Grep -n "^func \|^type "`,
  and `Read` with offset/limit.
- Keep every `init()` and every package-level `var` with an initializer in exactly one
  file; order of `init()` across files follows filename order, so moving one silently
  changes initialization order.

## Workflow mode

For many clusters or several files: `/refactor-go:split-file-workflow` with
`{"files": ["pkg/big.go", ...]}` or say "use a workflow to split pkg/big.go".

## References
- `references/toolchain.md`, `references/playbook.md`, `references/model-roles.md`.
