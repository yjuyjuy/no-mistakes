#!/usr/bin/env bash
# Materialize per-ticket-size no-mistakes validation profiles.
#
# Each profile is a separately initialized NM_HOME root with its own
# config.yaml, so a dispatcher can validate a task with rigor proportional to
# its size by exporting the matching NM_HOME before invoking no-mistakes.
#
# This is the right lever for per-size tuning: NM_HOME moves ALL no-mistakes
# state (config, gate repos, worktrees, database, socket), and each root's
# config.yaml combines a default model with `agent_args_override_per_step` to
# tune agent effort per pipeline step. It does NOT combine pipeline steps into
# one agent session, which is explicitly unsupported.
#
# Usage:
#   scripts/nm-size-profiles.sh init [small|medium|large|all]
#       Create the profile root(s) and drop each config.yaml in place.
#   scripts/nm-size-profiles.sh home <small|medium|large>
#       Print the NM_HOME path for a profile (for `export NM_HOME=$(...)`).
#   scripts/nm-size-profiles.sh doctor [small|medium|large|all]
#       Run `no-mistakes doctor` under each profile root to confirm it loads.
#
# The profile roots live under ${NM_PROFILES_ROOT:-$HOME/.no-mistakes-profiles}.
# Set NM_PROFILES_ROOT to relocate them (for example onto a fast scratch disk).
#
# See docs/src/content/docs/guides/size-profiles.md for the full model.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEMPLATE_DIR="$REPO_ROOT/contrib/size-profiles"
PROFILES_ROOT="${NM_PROFILES_ROOT:-$HOME/.no-mistakes-profiles}"
SIZES=(small medium large)

die() { echo "error: $*" >&2; exit 1; }

is_size() {
  local s="$1"
  for known in "${SIZES[@]}"; do [ "$known" = "$s" ] && return 0; done
  return 1
}

profile_home() {
  echo "$PROFILES_ROOT/$1"
}

resolve_sizes() {
  local arg="${1:-all}"
  if [ "$arg" = "all" ]; then
    printf '%s\n' "${SIZES[@]}"
  elif is_size "$arg"; then
    echo "$arg"
  else
    die "unknown size '$arg' (want: small, medium, large, or all)"
  fi
}

nm_bin() {
  command -v no-mistakes || die "no-mistakes not found on PATH"
}

cmd_init() {
  local sizes; sizes="$(resolve_sizes "${1:-all}")"
  while IFS= read -r size; do
    local home; home="$(profile_home "$size")"
    local template="$TEMPLATE_DIR/$size.config.yaml"
    [ -f "$template" ] || die "missing template $template"
    mkdir -p "$home"
    cp "$template" "$home/config.yaml"
    echo "materialized $size profile -> $home/config.yaml"
    echo "  validate a repo under it with:"
    echo "    NM_HOME=$home no-mistakes init   # once per repo"
    echo "    NM_HOME=$home no-mistakes axi run --intent \"...\""
  done <<< "$sizes"
}

cmd_home() {
  local size="${1:-}"
  is_size "$size" || die "usage: $0 home <small|medium|large>"
  profile_home "$size"
}

cmd_doctor() {
  local nm; nm="$(nm_bin)"
  local sizes; sizes="$(resolve_sizes "${1:-all}")"
  local failed=0
  while IFS= read -r size; do
    local home; home="$(profile_home "$size")"
    [ -f "$home/config.yaml" ] || die "profile '$size' not initialized; run: $0 init $size"
    echo "== $size ($home) =="
    if NM_HOME="$home" "$nm" doctor; then
      echo "  OK: $size config loaded"
    else
      echo "  FAIL: $size doctor returned non-zero" >&2
      failed=1
    fi
  done <<< "$sizes"
  return "$failed"
}

main() {
  local action="${1:-}"; shift || true
  case "$action" in
    init)   cmd_init "${1:-all}" ;;
    home)   cmd_home "${1:-}" ;;
    doctor) cmd_doctor "${1:-all}" ;;
    *)
      cat >&2 <<EOF
usage: $0 <init|home|doctor> [small|medium|large|all]

  init [size]     materialize profile root(s) with their config.yaml
  home <size>     print the NM_HOME path for a profile
  doctor [size]   run 'no-mistakes doctor' under each profile root

profile roots live under: $PROFILES_ROOT
override with NM_PROFILES_ROOT.
EOF
      exit 2
      ;;
  esac
}

main "$@"
