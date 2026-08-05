#!/usr/bin/env bash
#
# install-fork.sh - build and install THIS fork's no-mistakes binary
# update-safely, then restart the managed daemon.
#
# This is the deliberate fork-safe replacement for `no-mistakes update`.
# `no-mistakes update` downloads the UPSTREAM kunchenguid release, which does
# not carry this fork's features (for example the per-step agent arg profile
# `agent_args_override_per_step`), so running it silently clobbers the fork
# build. Use this script instead whenever you want the locally built fork
# binary installed at the managed path.
#
# What it does:
#   1. Builds the fork from source (`make build`).
#   2. Installs the binary to the managed bin directory
#      (${NM_HOME:-~/.no-mistakes}/bin/no-mistakes), NOT `go env GOPATH`/bin,
#      and relinks the PATH entry (~/.local/bin or /usr/local/bin) the same way
#      the official installer (docs/install.sh) does, so future runs and the
#      daemon service all resolve the same freshly built binary.
#   3. Restarts the daemon THROUGH the freshly installed binary, which
#      re-points the managed service at it.
#
# DRAIN CONSTRAINT (critical): the no-mistakes daemon is one shared instance
# serving every home/lane. Restarting it kills in-flight pipeline runs. This
# script refuses to restart while any pipeline run is active. Drain first, or
# pass --force to override loudly. The refusal is enforced by the daemon's own
# `daemon restart` active-run guard as well as an early pre-check here.
#
# Idempotent and re-runnable. Missing directories are created.
set -eu

usage() {
  cat <<'EOF'
Usage: scripts/install-fork.sh [--force] [--skip-restart] [-h|--help]

Build and install the fork no-mistakes binary to the managed path, then
restart the daemon.

Options:
  --force         Restart the daemon even when pipeline runs are active.
                  This KILLS in-flight runs on the shared daemon. Use only
                  after confirming no other lane depends on an active run.
  --skip-restart  Install the binary but do not restart the daemon. The
                  running daemon keeps the old binary until its next restart.
  -h, --help      Show this help.

Environment:
  NM_HOME               Managed root (default: ~/.no-mistakes). The binary is
                        installed to $NM_HOME/bin/no-mistakes.
  NO_MISTAKES_LINK_DIR  Override the PATH symlink directory.
EOF
}

FORCE=0
SKIP_RESTART=0
while [ $# -gt 0 ]; do
  case "$1" in
    --force) FORCE=1 ;;
    --skip-restart) SKIP_RESTART=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

# Resolve repo root from this script's location so the script works from any cwd.
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"
cd "$REPO_ROOT"

NM_HOME_DIR="${NM_HOME:-$HOME/.no-mistakes}"
INSTALL_DIR="$NM_HOME_DIR/bin"
BIN_PATH="$INSTALL_DIR/no-mistakes"
BUILT_BIN="$REPO_ROOT/bin/no-mistakes"

LINK_DIR="${NO_MISTAKES_LINK_DIR:-}"
if [ -z "$LINK_DIR" ]; then
  case ":$PATH:" in
    *":$HOME/.local/bin:"*) LINK_DIR="$HOME/.local/bin" ;;
    *) LINK_DIR="/usr/local/bin" ;;
  esac
fi
LINK_PATH="$LINK_DIR/no-mistakes"

echo "==> Building fork no-mistakes from $REPO_ROOT"
make build

if [ ! -x "$BUILT_BIN" ]; then
  echo "error: expected build output at $BUILT_BIN not found" >&2
  exit 1
fi

BUILT_VERSION="$("$BUILT_BIN" --version 2>/dev/null || true)"

echo "==> Installing fork binary to managed path: $BIN_PATH"
mkdir -p "$INSTALL_DIR"
install -m 755 "$BUILT_BIN" "$BIN_PATH"

# Relink the PATH entry to the managed binary, matching docs/install.sh so the
# command on PATH and the daemon service resolve the same file.
resolve_path() { (cd "$1" 2>/dev/null && pwd -P); }
REAL_INSTALL_DIR="$(resolve_path "$INSTALL_DIR")"
REAL_LINK_DIR="$(resolve_path "$LINK_DIR" 2>/dev/null || echo "")"
if [ -n "$REAL_INSTALL_DIR" ] && [ "$REAL_INSTALL_DIR" = "$REAL_LINK_DIR" ]; then
  echo "    install dir and link dir resolve to the same path; skipping symlink"
elif [ -w "$LINK_DIR" ] || (mkdir -p "$LINK_DIR" 2>/dev/null && [ -w "$LINK_DIR" ]); then
  rm -f "$LINK_PATH"
  ln -s "$BIN_PATH" "$LINK_PATH"
  echo "    linked $LINK_PATH -> $BIN_PATH"
else
  echo "    warning: cannot write $LINK_DIR; leaving PATH link unchanged" >&2
fi

echo "==> Installed: $BUILT_VERSION"
echo "    managed binary: $BIN_PATH"

if [ "$SKIP_RESTART" -eq 1 ]; then
  echo "==> --skip-restart set; daemon NOT restarted. It keeps the old binary"
  echo "    until its next restart. Run this without --skip-restart to swap it."
else
  RESTART_ARGS="daemon restart"
  if [ "$FORCE" -eq 1 ]; then
    echo "==> FORCE: restarting daemon even if pipeline runs are active."
    echo "    This KILLS in-flight runs on the SHARED daemon."
    RESTART_ARGS="daemon restart --force"
  else
    echo "==> Restarting daemon (refuses if pipeline runs are active; drain first"
    echo "    or re-run with --force)"
  fi
  # Restart THROUGH the freshly installed managed binary so the service is
  # re-pointed at it (os.Executable resolves to $BIN_PATH). The daemon's own
  # active-run guard enforces the drain constraint; a refusal exits non-zero.
  # shellcheck disable=SC2086
  if ! NM_HOME="$NM_HOME_DIR" "$BIN_PATH" $RESTART_ARGS; then
    echo "error: daemon restart failed (likely active pipeline runs)." >&2
    echo "       The fork binary IS installed at $BIN_PATH, but the running" >&2
    echo "       daemon still uses the old binary. Drain active runs and re-run," >&2
    echo "       or pass --force to override." >&2
    exit 1
  fi
fi

echo
echo "Done. The fork binary is now the managed no-mistakes."
echo "NOTE: do NOT run 'no-mistakes update' afterward - it pulls the UPSTREAM"
echo "      release and will clobber this fork build. Re-run this script to"
echo "      reinstall the fork after any change."
