# DEMOLOG.md — see it work

Exact commands a human can run after each milestone. All from the repo root;
`make build` first.

## M1 — Tier 0 notify

```sh
make build

# one-shot diff against persisted state (~/.local/state/clauditor/);
# the very first run is a silent baseline
./bin/clauditor notify --once --format text

# make a change: dispatch a throwaway bg session somewhere
( cd /tmp && claude --bg "reply with the word ok" )

# now the diff reports it
./bin/clauditor notify --once --format text
# [session_new] (loose) · "reply with the word ok" · 5s
# …and when it finishes, a later run prints:
# [completed] (loose) · "…" · 1m

# streaming mode (Ctrl-C to stop): one line per state transition, 5s poll
./bin/clauditor notify --stream --format json

# exec mode: run any command per event
./bin/clauditor notify --stream --exec 'echo "$CLAUDITOR_EVENT $CLAUDITOR_REPO $CLAUDITOR_WAITING_FOR"'

# on the Mac (see docs/VERIFY.md #1):
CLAUDITOR_HOST=devbox ./scripts/mac-notify.sh
```

Verified live on the build box 2026-08-06: two consecutive `--once` runs
against the real fleet (6 interactive sessions) — first silent baseline,
second silent (no transitions), exit 0.

## M0 — recon artifacts

```sh
less docs/RESEARCH.md      # steal/adapt/avoid + empirical answers
less docs/REPLY.md         # the reply experiment transcript
./scripts/capture-fixtures.sh   # refresh fixtures after a claude upgrade
```
