#!/usr/bin/env bash
# check-architecture.sh - enforce this repository's hexagonal dependency rules
# and readability caps (300 lines/file, 40 lines/function, 3 nesting levels).
#
# Usage:
#   bash .agents/skills/ddd-hexagonal/scripts/check-architecture.sh
#   bash .agents/skills/ddd-hexagonal/scripts/check-architecture.sh --boundaries-only
#   bash .agents/skills/ddd-hexagonal/scripts/check-architecture.sh --caps-only
set -euo pipefail

usage() {
  sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'
}

run_boundaries=1
run_caps=1
for arg in "$@"; do
  case $arg in
    --boundaries-only) run_caps=0 ;;
    --caps-only) run_boundaries=0 ;;
    -h|--help) usage; exit 0 ;;
    *)
      printf 'unknown flag: %s\n' "$arg" >&2
      usage >&2
      exit 2
      ;;
  esac
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(git -C "$script_dir" rev-parse --show-toplevel 2>/dev/null || true)
if [[ -z $root ]]; then
  # .agents/skills/ddd-hexagonal/scripts -> repository root
  root=$(cd -- "$script_dir/../../../.." && pwd)
fi
cd "$root"

if [[ ! -f go.mod ]]; then
  printf 'error: go.mod not found under %s\n' "$root" >&2
  exit 2
fi

module=$(go list -m)
status=0

classify() {
  case $1 in
    "$module"/domain|"$module"/domain/*) printf 'domain' ;;
    "$module"/application|"$module"/application/*) printf 'application' ;;
    "$module"/infrastructure|"$module"/infrastructure/*) printf 'infrastructure' ;;
    "$module"/handlers|"$module"/handlers/*) printf 'delivery' ;;
    "$module"/web|"$module"/web/*) printf 'delivery' ;;
    *) printf 'other' ;;
  esac
}

is_third_party() {
  local imp=$1 first=${1%%/*}
  if [[ $imp == "$module" || $imp == "$module"/* ]]; then
    return 1
  fi
  [[ $first == *.* ]]
}

violation() {
  printf '%s: imports %s (%s)\n' "$1" "$2" "$3"
  status=1
}

check_boundaries() {
  local listed pkg imports imp layer packages=0
  if ! listed=$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./... 2>&1); then
    printf '%s\n' "$listed" >&2
    printf 'error: go list failed; fix build errors first\n' >&2
    exit 2
  fi

  while read -r pkg imports; do
    [[ -n ${pkg:-} ]] || continue
    layer=$(classify "$pkg")
    [[ $layer == other ]] && continue
    packages=$((packages + 1))

    for imp in ${imports:-}; do
      case $layer in
        domain)
          case $imp in
            "$module"/application*|"$module"/infrastructure*|"$module"/handlers*|"$module"/web*)
              violation "$pkg" "$imp" 'domain must not depend on outer layers'
              ;;
            *)
              if is_third_party "$imp"; then
                violation "$pkg" "$imp" 'domain must stay framework-free (stdlib + domain only)'
              fi
              ;;
          esac
          ;;
        application)
          case $imp in
            "$module"/infrastructure*|"$module"/handlers*|"$module"/web*)
              violation "$pkg" "$imp" 'application must not depend on adapters or delivery'
              ;;
            *)
              if is_third_party "$imp"; then
                violation "$pkg" "$imp" 'application must stay framework-free (stdlib + domain only)'
              fi
              ;;
          esac
          ;;
        infrastructure)
          case $imp in
            "$module"/application*|"$module"/handlers*|"$module"/web*)
              violation "$pkg" "$imp" 'infrastructure must not depend on application or delivery'
              ;;
          esac
          ;;
        delivery)
          case $imp in
            "$module"/infrastructure*)
              violation "$pkg" "$imp" 'delivery must use ports; wire adapters in the composition root'
              ;;
          esac
          ;;
      esac
    done
  done < <(printf '%s\n' "$listed")

  if ((status == 0)); then
    printf 'architecture ok: %d packages\n' "$packages"
  fi
}

if ((run_boundaries)); then
  check_boundaries
fi

if ((run_caps)); then
  if ! go run ./tools/caps .; then
    status=1
  fi
fi

exit "$status"
