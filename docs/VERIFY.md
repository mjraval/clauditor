# VERIFY.md — human checklist

Things that could **not** be fully verified in the build environment, with
exact commands. Everything else in `docs/RESEARCH.md` was verified live on
this machine on 2026-08-06 (Claude Code 2.1.223, tmux 3.4, git 2.54.0).

## Verified during the build (no action needed)

- `claude agents --json` lists interactive AND background sessions ✔
- Background schema incl. `state`, `waitingFor`, `id` captured live ✔
- tmux send-keys reply injection works (`docs/REPLY.md`) ✔
- `claude logs <id>` emits raw ANSI screen replay, no tail flag ✔
- Fleet fixtures captured via `scripts/capture-fixtures.sh` ✔

## UNVERIFIED — run these yourself

1. **Mac notification client end-to-end** (build box has no Mac):
   ```sh
   # on the Mac
   CLAUDITOR_HOST=<your-ssh-host> ./scripts/mac-notify.sh
   # then, on the server, make a session change state (e.g. dispatch a
   # trivial bg session: cd /tmp && claude --bg "reply ok")
   # expect: a macOS notification within ~10s
   ```
   Also verify reconnect: close the laptop lid 2 minutes, reopen, expect the
   loop to resume without stacked duplicate processes
   (`pgrep -fl mac-notify` should show one instance).

2. **Cloudflare Access JWT validation against the real tunnel** (build box
   has no Access-fronted hostname for clauditor yet):
   ```sh
   # after deploy/ACCESS.md setup:
   curl -s https://clauditor.<domain>/api/v1/state   # expect Access login redirect / 403 without token
   # from an authenticated browser session it must return the snapshot
   ```
   The JWKS fetch + validation logic is unit-tested with a synthetic JWKS;
   what needs a human is the real team-domain/AUD pair in config.

3. **systemd user service on the real box** (can't enable linger inside this
   build session):
   ```sh
   mkdir -p ~/.config/systemd/user && cp deploy/systemd/clauditor.service ~/.config/systemd/user/
   systemctl --user daemon-reload && systemctl --user enable --now clauditor
   loginctl enable-linger $USER
   systemctl --user status clauditor
   ```

4. **Supervisor idle-stop behavior (~1h)**: docs say idle unattached bg
   sessions stop after ~1h and restart on demand. Not observable in a build
   session. Watch a `done` session for an hour and confirm clauditor's
   differ emits nothing spurious (it treats supervisor-initiated stop of a
   `done` session as no event because `done` is terminal).

5. **`claude agents --json` under supervisor restart / version upgrade**:
   after a Claude Code upgrade, re-run:
   ```sh
   ./scripts/capture-fixtures.sh
   go test ./internal/collect/...
   ```
   and commit the new fixture file (`agents_v<newver>.json`). Parser is
   version-tolerant but fixtures should track reality.

6. **Race detector in CI**: `go test -race` requires cgo (a C compiler);
   this build box has none. If you set up CI, add a `-race` job on a
   runner with gcc — the TUI's concurrency was verified by static tracing
   (2026-08-09 QA), but the detector should back that up on every change.
