#!/usr/bin/env bash
# capture-fixtures.sh — snapshot real `claude agents --json`, tmux, and git
# worktree output into internal/collect/testdata/, redacting prompt-derived
# names. Run on a machine with live sessions; commit the results so parser
# tests are table-driven over real schema versions.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/internal/collect/testdata"
mkdir -p "$OUT"

ver="$(claude --version 2>/dev/null | awk '{print $1}')"
[ -n "$ver" ] || { echo "claude not found" >&2; exit 1; }

# Redact: session names may embed prompt text; replace with a placeholder
# while keeping structure. Keep cwd (paths are not secret on a dev box, and
# correlation tests need realistic paths); adjust the jq if yours differ.
claude agents --json 2>/dev/null \
  | jq '[.[] | .name = (if .name then "redacted-name-\(.pid // .id)" else null end)]' \
  > "$OUT/agents_v${ver}.json"
echo "wrote $OUT/agents_v${ver}.json"

claude agents --all --json 2>/dev/null \
  | jq '[.[] | .name = (if .name then "redacted-name-\(.pid // .id)" else null end)]' \
  > "$OUT/agents_all_v${ver}.json"
echo "wrote $OUT/agents_all_v${ver}.json"

if tmux list-panes -a -F $'#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_id}\t#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}\t#{window_name}\t#{session_attached}' >/dev/null 2>&1; then
  tmux list-panes -a -F $'#{session_name}\t#{window_index}\t#{pane_index}\t#{pane_id}\t#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}\t#{window_name}\t#{session_attached}' \
    > "$OUT/tmux_panes_$(tmux -V | tr ' ' '_').txt"
  echo "wrote tmux panes fixture"
fi

# First repo argument (or current repo) → worktree porcelain fixture
repo="${1:-$ROOT}"
if git -C "$repo" rev-parse --git-dir >/dev/null 2>&1; then
  git -C "$repo" worktree list --porcelain > "$OUT/git_worktree_porcelain.txt"
  echo "wrote git worktree fixture from $repo"
fi
