#!/usr/bin/env bash
# context-map.sh - print the internal package dependency graph as Mermaid.
#
# Usage:
#   bash .agents/skills/ddd-hexagonal/scripts/context-map.sh > context-map.md
#
# Read-only: it never edits the repository. Useful to spot boundary leaks and
# to explain the bounded contexts during a review.
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(git -C "$script_dir" rev-parse --show-toplevel 2>/dev/null || true)
if [[ -z $root ]]; then
  root=$(cd -- "$script_dir/../../../.." && pwd)
fi
cd "$root"

if [[ ! -f go.mod ]]; then
  printf 'error: go.mod not found under %s\n' "$root" >&2
  exit 2
fi

module=$(go list -m)
listed=$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./...)

printf 'flowchart LR\n'

while read -r pkg imports; do
  [[ -n ${pkg:-} ]] || continue
  case $pkg in
    "$module"|"$module"/*) ;;
    *) continue ;;
  esac

  from=${pkg#"$module"}
  from=${from#/}
  from=${from:-root}
  from_id=${from//\//_}

  for imp in ${imports:-}; do
    case $imp in
      "$module"|"$module"/*) ;;
      *) continue ;;
    esac
    [[ $imp == "$pkg" ]] && continue

    to=${imp#"$module"}
    to=${to#/}
    to=${to:-root}
    to_id=${to//\//_}
    printf '  %s["%s"] --> %s["%s"]\n' "$from_id" "$from" "$to_id" "$to"
  done
done < <(printf '%s\n' "$listed")
