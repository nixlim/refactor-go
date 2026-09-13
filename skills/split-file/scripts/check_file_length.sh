#!/usr/bin/env bash
# Fail when a Go file exceeds a code-line budget (blank and comment-only lines excluded).
#   check_file_length.sh [--max N] [--baseline FILE] [paths...]     check (exit 1 on violation)
#   check_file_length.sh --write-baseline FILE [--max N] [paths...]  grandfather files currently over budget
#   check_file_length.sh --hook                                       Claude Code PostToolUse mode (stdin JSON, exit 2 on violation)
# Baseline format: one "<path>\t<code_lines>" per line. Grandfathered files may shrink, never grow.
set -u
MAX=${REFACTOR_MAX_LINES:-500}; BASE=${REFACTOR_BASELINE:-.refactor-baseline.tsv}; HOOK=0; WRITE=""
PATHS=()
while [ $# -gt 0 ]; do case "$1" in
  --max) MAX=$2; shift 2;; --baseline) BASE=$2; shift 2;; --hook) HOOK=1; shift;; --write-baseline) WRITE=$2; shift 2;;
  *) PATHS+=("$1"); shift;; esac; done
[ ${#PATHS[@]} -eq 0 ] && PATHS=(.)

code_lines() { # skip blank, // lines, and /* */ blocks (line-granular)
  awk 'BEGIN{n=0;inb=0} { s=$0; gsub(/^[ \t]+|[ \t]+$/,"",s)
    if(inb){ if(index(s,"*/")){inb=0}; next }
    if(s==""||s ~ /^\/\//) next
    if(s ~ /^\/\*/){ if(!index(s,"*/")) inb=1; next }
    n++ } END{print n}' "$1"; }
list_go() { for p in "${PATHS[@]}"; do if [ -f "$p" ]; then echo "$p"; else find "$p" -name '*.go' -not -path '*/vendor/*' -not -path '*/.git/*' -not -path '*/testdata/*'; fi; done; }
baseline_for() { [ -f "$BASE" ] && awk -F'\t' -v f="$1" '$1==f{print $2}' "$BASE"; }

if [ -n "$WRITE" ]; then
  : > "$WRITE"; c=0
  while read -r f; do n=$(code_lines "$f"); if [ "$n" -gt "$MAX" ]; then printf '%s\t%s\n' "${f#./}" "$n" >> "$WRITE"; c=$((c+1)); fi; done < <(list_go)
  echo "baseline written: $c files over $MAX lines -> $WRITE"; exit 0
fi

check_one() { # prints violation, returns 1 on violation
  local f=$1 n allowed; n=$(code_lines "$f"); allowed=$(baseline_for "${f#./}")
  if [ -n "$allowed" ]; then [ "$n" -gt "$allowed" ] && { echo "$f: grandfathered at $allowed code lines but grew to $n"; return 1; }
  elif [ "$n" -gt "$MAX" ]; then echo "$f: $n code lines > budget $MAX"; return 1; fi
  return 0; }

if [ $HOOK -eq 1 ]; then
  payload=$(cat)
  if command -v python3 >/dev/null 2>&1; then fp=$(printf '%s' "$payload" | python3 -c 'import json,sys;print((json.load(sys.stdin).get("tool_input") or {}).get("file_path",""))' 2>/dev/null)
  else fp=$(printf '%s' "$payload" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1); fi
  case "$fp" in *.go) ;; *) exit 0;; esac
  [ -f "$fp" ] || exit 0
  rel=$fp; case "$fp" in /*) rel=$(realpath --relative-to="$PWD" "$fp" 2>/dev/null || echo "$fp");; esac
  if msg=$(check_one "$rel"); then exit 0; else
    echo "FILE SIZE GUARD: $msg. Do not keep adding to this file. Split it first (/refactor-go:split-file), then continue." >&2; exit 2; fi
fi

viol=0; total=0
while read -r f; do total=$((total+1)); check_one "$f" || viol=$((viol+1)); done < <(list_go)
if [ $viol -gt 0 ]; then echo "$viol file(s) violate the size budget (max $MAX, baseline $BASE)"; exit 1; fi
echo "ok: $total files within budget (max $MAX)"
