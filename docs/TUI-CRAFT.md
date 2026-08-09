# TUI-CRAFT.md — implementation techniques from the craft studies

Companion to TUI-DESIGN.md (the *what*); this is the *how*, distilled from
deep teardowns of agent-deck (MIT — code portable with attribution) and
YoanWai/agent-manager (Apache-2.0 — code portable with NOTICE propagation),
both cloned read-only under `references/`. File:line pointers reference
those clones. Port tags: [AD] agent-deck, [AM] agent-manager.

## 1. Preview rendering — the correct mechanism

Our v1 preview stripped ANSI from `claude logs` output. That is
unfixable-by-stripping: `logs` replays a token stream whose only layout
information IS the escape sequences; stripping mashes words together.
Both reference tools converged on the same answer:

**Source per session kind:**
- Session has a tmux pane → `tmux capture-pane -p -e` — tmux hands back an
  already-rendered, already-wrapped 2D grid. Nothing to reflow.
  [AD internal/tmux/tmux.go:3331,3397] [AM internal/tmux/tmux.go:334]
- Session has NO pane (pure background) → **transcript tail**, not logs:
  parse `${CLAUDE_CONFIG_DIR:-~/.claude}/projects/*/<sessionId>.jsonl`,
  render the last user/assistant messages as `❯ …` / `● …` lines.
  Verified locally: entries are `{"type":"assistant","message":{"content":
  [{"type":"text","text":…}]}}` etc. Tolerant parsing (same discipline as
  the agents-json parser); skip non-message types (`ai-title`, `mode`,
  `attachment`, tool_use blocks render as dim one-liners like `⚒ Edit …`).
- `claude logs` is no longer a preview source (stays for the API/logs
  pager where the user asked for raw output).

**ANSI passthrough sanitation kit** (apply per captured line, in order) —
this is what makes keeping colors SAFE instead of catastrophic:
1. Strip display-damaging sequences: regex
   `\x1b\[[0-9;?]*[KJLMSTr]` + 7-bit `\x1b[DEM]` — erase-line/screen,
   scroll, insert/delete-line, scroll-region; these would scroll or clear
   the HOST terminal when embedded in View() output.
   [AM view.go:347-389] [AD home.go:18684-18700]
2. Drop C0 controls except ESC and TAB (kills `\r`/`\b` layout corrupters).
   [AD home.go:18665-18676]
3. Width-clamp with `charmbracelet/x/ansi`'s `ansi.Truncate` (escape-aware),
   then append `\x1b[0m` to any line containing ESC before padding —
   otherwise an unterminated SGR bleeds into the neighboring column and the
   next frame. [AD home.go:18514-18523] [AM view.go:381-386]
4. Collapse runs of >2 blank lines; trim trailing blanks before height
   truncation so limited height shows content, not cursor padding.
   [AD home.go:18367-18372,18442-18477]

**Cadence:** adaptive — ~300ms refresh while the selected session is
working, ~1200ms when calm; 50ms settle debounce after cursor movement and
a generation counter so a held `j` discards stale captures instead of
queueing tmux forks. [AM model.go:404-454]

## 2. Visual system — tonal surfaces, not borders

The single biggest "looks designed" lever [AM surface.go:10-34]:
- Never paint the backdrop — terminal background shows through.
- Structure = three derived tones + hairlines, from a `mix(a,b,t)` lerp
  over the theme anchors: `panel = mix(Bg,Surface,0.55)` (list rail fill),
  `block = mix(Bg,Surface,0.35)` (sheets/toasts), `selected = Surface`,
  `rule = mix(Bg,Text,0.22)` (hairline seam). [AM theme.go:456-469]
- The `paint(s,width,bg)` primitive: pad to exact display width AND
  re-assert the bg sequence after every inner `\x1b[0m`, or per-segment
  lipgloss renders drop the fill mid-row. [AM surface.go:169-193]
- Panel titles = bold accent title + full-width `─` rule, exactly 2 lines —
  a panel read with zero border chrome. [AD home.go:14290-14314]
- Selected row = one unbroken inverted bar (bg=accent, fg=bg, bold) across
  EVERY segment of the row incl. glyph and badges, plus `▶` prefix.
  [AD styles.go:486-504, home.go:16431-16449]

**Style discipline:** palette as a struct of ~12 semantic roles; all
lipgloss styles pre-allocated once in an `initStyles()` (never
`NewStyle()` per row per frame). [AD styles.go:22-61,297-555]

**Width discipline (fixes our overflow bug):** pre-join column equalization
must use `lipgloss.Width` (agrees with JoinHorizontal's math); post-join
clamping uses `ansi.StringWidth`-based truncation. One-cell disagreement
=> lipgloss pads to the wider measure => frame wraps => stacked/duplicated
content. [AD cellwidth.go:26-105, home.go:14674-14689]

## 3. Density & rows

- Budget-based truncation: sum the display width of every fixed element
  (gutter, indent, glyph, badges, age), give the REMAINDER to the name —
  never a fixed 24-char cap. [AD home.go:16593-16630]
- Two-component humanized times: `just now`, `45m ago`, `3h 20m`, `2d 5h`,
  `5mo 1w`; quantize sub-minute ages to 5s steps (a column ticking every
  second is motion the eye chases for nothing). [AD humanize_since.go:18-54]
  [AM view.go:559-573]
- Scroll affordances: `⋮ +N above` / `⋮ +N below` rows, each a real
  reserved line. [AD home.go:15975-16042]
- Empty states: responsive tiers (full/compact/minimal) chosen from BOTH
  width and height. [AD home.go:14478-14585]

## 4. Feedback & feel

- Toasts spliced over the top-right of the already-rendered frame
  (ansi.Truncate head + ansi.TruncateLeft tail) — notices cost zero layout
  rows, nothing ever shifts. [AM toast.go]
- Optimistic placeholder rows for slow creations (spinner + italic title +
  elapsed counter), reconciled when the real session appears.
  [AD home.go:16288-16325]
- Footer hints carry live state in their labels (`tab tool: claude`) and
  are tiered: selection verbs first, view keys dim behind them.
  [AM view.go:433-544, legend.go:32-84]
- Never render a blank frame: return the last rendered frame while
  suspended into an attach. [AD home.go:13864-13889]

## 5. Deferred to v2b (bigger machinery, next wave)

- tmux control-mode client (`tmux -C`) for event-push previews — zero
  polling, zero forks; block-tag matching and greeting-skip are the two
  hard parts. [AM internal/tmux/control.go, focuswatch.go]
- Focus mode: keyboard routed into the pane (send-keys -H hex) while the
  cockpit stays on screen; simulated blinking cursor spliced by display
  column. [AM focuskeys.go:100-195, focussel.go:343-379]
- `n` new interactive session in tmux (workmux adopt-and-switch recipe;
  launch via a written sh script — send-keys truncates ~1024 bytes).
  [AM tmux.go:86-143]
- `h` resume picker powered by the same transcript parser as the preview.
- Persisted last-screen snapshot for dead sessions. [AM poller.go:199-215]
- Light-theme capture island; OSC 11 background sync; sextant sub-cell
  edges. [AM capturebackdrop.go, surface.go:47-146]

## Attribution

Anything ported closely carries `// adapted from agent-deck (MIT)` or
`// adapted from agent-manager (Apache-2.0)` inline, plus NOTICE entries.
