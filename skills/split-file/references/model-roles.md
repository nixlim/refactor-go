# Model roles (refactor-go)

Allowed models: `claude-fable-5-1`, `claude-opus-5`, `claude-opus-4-8` (all 1M context;
choose on judgment, independence, and cost, not window size).

| Role | Agent | Model | Why |
|---|---|---|---|
| Orchestrator | main session | Fable 5.1 or Opus 5 | Holds the plan; delegates everything noisy. |
| Planner | `split-planner` | Fable 5.1, effort high | The seam decision is the hardest judgment; one call. |
| Plan critic | `plan-critic` | Opus 5 | Different model than the planner so blind spots differ. |
| Extractors (parallel) | `extractor` | Opus 4.8, `isolation: worktree` | Mechanical and compiler-gated; cheapest tier; escalates to Opus 5 on second failure. |
| Gate runner | `gate-runner` | Opus 4.8, effort low | Keeps `go test` output out of the orchestrator. |
| Second-opinion reviewer | `scripts/codex_review.sh` (headless OpenAI Codex, not a Claude subagent) | **GPT-5.6 Sol**, reasoning effort `ultra` (falls back to `xhigh` on API-key backends) | A different model *family* reviews first. Cross-family review catches errors a same-family reviewer shares; the codex-orchestrator research notes warn that blind consensus is a popularity trap, so its verdict is evidence to adjudicate, not a vote. |
| Adjudicating reviewer | `refactor-reviewer` | Fable 5.1, effort high | Independent behavior review; never the extractor's model. |

Escalation ladder per cluster: resume → Opus 5 → planner re-plans smaller → stop.
Overrides: `CLAUDE_CODE_SUBAGENT_MODEL=... CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1`, or copy an
agent file into `.claude/agents/` and edit `model:`.

## Review order and why
1. **Codex (GPT-5.6 Sol, ultra) reviews first**, blind, from the diff and the plan.
2. **Fable 5.1 reviews second**, doing its own check *before* reading Codex's verdict (to avoid
   anchoring), then confirms or refutes each Codex finding with evidence.
3. **One consensus round** on refuted findings via `codex exec resume` (the script's
   `--followup`). Unresolved disagreements go to the user with both sides' evidence.
Override the Codex model/effort with `CODEX_REVIEW_MODEL` / `CODEX_REVIEW_EFFORT`.
