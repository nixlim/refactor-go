# Playbook: decomposing a Go god-file

## Same-package split first
Because Go scopes identifiers to the package, splitting a 12K-line `engine.go` into
`engine.go`, `engine_scheduler.go`, `engine_io.go`, `engine_types.go` changes zero call
sites. Do this first, always. It removes the parallel-agent bottleneck (agents edit
different files) and the context problem (each file is readable in one call). Consider a
sub-package only afterwards, and only for clusters with a clean exported surface.

## Choosing seams
1. **A type and all its methods move together.** `inventory` glues methods to their
   receiver; never split a type's methods across files unless the file is still over
   budget, in which case split by method group (e.g. `server_http.go`, `server_grpc.go`)
   and say so in the plan.
2. **Leaves first** (decls that reference nothing else in the file).
3. **Responsibility over size**: `types.go`, `errors.go`, `config.go`, `io.go`,
   `<feature>.go`. A file should be describable in one sentence.
4. **Package-level state and `init()` stay in one owning file.** `init()` order across
   files is filename order; moving one changes initialization order. Keep all `init()`
   in `init.go` or the main file and note it in the plan.
5. **Target 150–400 code lines per file**, hard cap 500.
6. **Tests**: `_test.go` files that test moved decls stay valid (same package). If a
   test file is itself over budget, split it the same way.

## Sub-package extraction (only when justified)
Signals it is justified: the cluster is used by other packages already, or has a stable
exported API, and references nothing unexported in the parent. Procedure: movedecl to
`<dir>/<sub>/`, `gopls rename` to export what the parent needs, goimports, gate.

**Import cycles**: if the sub-package needs the parent, the compiler says
`import cycle not allowed`. The only acceptable answers during a split are (a) move the
needed decl into the sub-package too, if it belongs there, or (b) abandon the sub-package
extraction and keep the same-package split. Adding an interface, a callback parameter,
a registry, or a `common`/`internal/shared` package to break the cycle is a design change
and is out of scope; report it.

## Package-level state
`var registry = map[string]Handler{}`, `var once sync.Once`, `var client = New()`:
exactly one file owns each. Duplicating a `var` across files does not compile (redeclared),
which the gate catches, but duplicating *under a new name* does compile and forks the
state; the reviewer looks for that.

## Reading large files without paying for them
Claude Code's Read tool truncates around 2,000 lines / 25K tokens per call and returns
partial content silently. Use `.refactor/inventory.md`, `Grep -n "^func \|^type \|^var \|^const "`,
and `Read` with `offset`/`limit` on one decl at a time. The approved models have 1M
windows; loading a whole god-file is still a mistake because quality drops with context
length and because a worker that holds the whole file reasons less precisely than one
that holds its cluster.
