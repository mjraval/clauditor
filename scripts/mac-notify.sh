#!/usr/bin/env bash
# mac-notify.sh — run on the Mac. Streams clauditor events over SSH and posts
# macOS notifications. Survives laptop sleep via a reconnect loop with
# backoff; a pidfile prevents stacked duplicate instances. Zero config
# beyond CLAUDITOR_HOST (an ssh host alias; works unchanged through a
# `ProxyCommand cloudflared access ssh` config).
#
# usage: CLAUDITOR_HOST=devbox ./mac-notify.sh
set -u

HOST="${CLAUDITOR_HOST:-}"
if [ -z "$HOST" ]; then
  echo "mac-notify: set CLAUDITOR_HOST to your ssh host alias" >&2
  exit 1
fi

command -v jq >/dev/null 2>&1 || { echo "mac-notify: jq is required (brew install jq)" >&2; exit 1; }

PIDFILE="${TMPDIR:-/tmp}/clauditor-mac-notify.pid"
if [ -f "$PIDFILE" ]; then
  oldpid="$(cat "$PIDFILE" 2>/dev/null || true)"
  if [ -n "$oldpid" ] && kill -0 "$oldpid" 2>/dev/null; then
    echo "mac-notify: already running (pid $oldpid)" >&2
    exit 0
  fi
fi
echo $$ > "$PIDFILE"
trap 'rm -f "$PIDFILE"' EXIT

notify() {
  # $1 title, $2 body
  if command -v terminal-notifier >/dev/null 2>&1; then
    terminal-notifier -title "$1" -message "$2" -group clauditor
  else
    osascript -e "display notification \"${2//\"/\\\"}\" with title \"${1//\"/\\\"}\"" 2>/dev/null
  fi
}

backoff=2
while :; do
  # Process substitution (not a pipeline) so `backoff=2` below survives the
  # loop — a pipeline would run the while-body in a subshell (SC2030/31).
  while IFS= read -r line; do
    ev=$(jq -r '.event // empty' <<<"$line" 2>/dev/null) || continue
    [ -n "$ev" ] || continue
    repo=$(jq -r '.session.repo // "?"' <<<"$line")
    name=$(jq -r '.session.name // ""' <<<"$line")
    waiting=$(jq -r '.session.waitingFor // ""' <<<"$line")
    case "$ev" in
      needs_input) title="🔶 Claude needs input" ;;
      completed)   title="✅ Claude finished" ;;
      failed)      title="❌ Claude failed" ;;
      *)           continue ;;  # new/gone/appeared are noise on a phone-adjacent device
    esac
    body="$repo"
    [ -n "$name" ] && body="$body · $name"
    [ -n "$waiting" ] && body="$body · $waiting"
    notify "$title" "$body"
    backoff=2  # a delivered event proves the link is healthy
  done < <(ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=3 -o BatchMode=yes \
      "$HOST" 'clauditor notify --stream --format json' 2>/dev/null)
  # Connection dropped (sleep, network, server restart): back off and retry.
  sleep "$backoff"
  backoff=$(( backoff * 2 ))
  [ "$backoff" -gt 60 ] && backoff=60
done
