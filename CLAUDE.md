# CLAUDE.md — working in this repo

clauditor is a single Go binary that aggregates `claude agents --json` +
tmux + git worktrees into one fleet snapshot, served over CLI/TUI/HTTP/SSE.
`SPEC.md` is the authority; `docs/ARCHITECTURE.md` explains the shape and
records every deliberate divergence from the spec.

## Toolchain

Go, golangci-lint, and shellcheck are NOT on the default PATH:

```sh
export PATH=$HOME/.local/go/bin:$HOME/.local/bin:$PATH
```

or just use the Makefile, which resolves the fallbacks itself:

```sh
make build     # → bin/clauditor (static, version-stamped)
make test      # offline; uses test/stubbin fakes
make lint      # golangci-lint + shellcheck (both must be clean)
```

All three must stay green. Never let a test touch the network.

## Ground rules

- **The snapshot is the product.** All state derivation happens once, in
  `internal/model/correlate.go`. CLI/TUI/Web/API are renderers — never put
  state logic in a renderer.
- **Supervisor state is authoritative.** Never infer session state from
  pane text; `tmux.heuristics` is a reserved config key, deliberately
  unimplemented.
- **Never weaken SPEC §9**: loopback bind, Access JWT on every /api route,
  actions off by default, no permission-bypass flags ever (the deny-list in
  `internal/actions/actions.go` must stay ahead of new claude spellings).
  Never put argv/prompt content in error messages returned to clients.
- **Exec discipline**: argv arrays only (no shell strings), context
  timeouts on every external command, explicit `Dir`.
- `references/` is read-only prior art; AGPL repo (claude-tmux) is
  ideas-only — no code, identifiers, or regexes from it. Attribution for
  MIT ports goes in `NOTICE`.
- Conventional commits, one per coherent change; update `docs/DEMOLOG.md`
  when behavior a human can run changes.

## Testing conventions

- Fake `claude`/`tmux` binaries live in `test/stubbin/` (env:
  `CLAUDITOR_STUB_FIXTURE`, `CLAUDITOR_STUB_LOG`, `CLAUDITOR_STUB_PANES`);
  tests prepend them to PATH — see `internal/notify/run_test.go`.
- Real captured schema fixtures live in `internal/collect/testdata/`;
  after a Claude Code upgrade run `./scripts/capture-fixtures.sh` and add
  the new `agents_v<ver>.json` to the parser test table.
- Parser must stay version-tolerant: unknown fields ignored, one corrupt
  element never drops the snapshot, identity-less entries dropped.

## Verified environment facts (don't re-derive; see docs/RESEARCH.md)

- `claude agents --json` lists interactive AND background sessions.
- `name` is mutable display text (supervisor renames it); identity is
  `sessionId`.
- `claude logs` = raw ANSI screen replay, no tail flag — strip and cap.
- Reply-by-tmux-injection works and ships gated off
  (`actions.experimental_reply`); see `docs/REPLY.md` before touching
  `internal/actions/reply.go`.

## What's next

Tier 2 work starts from `docs/ROADMAP.md` (browser terminal, transcript
search, merge-back helper, …). Anything unverifiable in a build sandbox
goes in `docs/VERIFY.md` as exact commands for a human.
