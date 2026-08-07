// clauditor WebUI — vanilla ES module, no dependencies (SPEC §10).
// Read-only fleet board (M4) + gated actions (M5).

const $ = (sel) => document.querySelector(sel);

const state = {
  snapshot: null,
  cfg: { actionsEnabled: false, experimentalReply: false, worktreeUrlTemplate: "" },
  filter: null,          // null | needs | working | done
  drawerKey: null,
  staleSince: null,
};

// ---------- helpers ----------

const BUCKETS = [
  { id: "needs",    title: "needs input", match: (s) => s.state === "blocked" || !!s.waitingFor },
  { id: "working",  title: "working",     match: (s) => s.state === "working" },
  { id: "idle",     title: "idle / interactive", match: (s) => s.state === "idle" || s.state === "unknown" },
  { id: "done",     title: "done / failed / stopped", match: () => true, collapsed: true },
];

function bucketOf(s) {
  for (const b of BUCKETS) if (b.match(s)) return b.id;
  return "done";
}

function chipClass(s) {
  if (s.state === "blocked" || s.waitingFor) return "blocked";
  return s.state || "unknown";
}

function age(s) {
  const secs = s.ageSeconds || 0;
  if (secs <= 0) return "";
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 48 * 3600) return `${Math.floor(secs / 3600)}h${String(Math.floor(secs / 60) % 60).padStart(2, "0")}m`;
  return `${Math.floor(secs / 86400)}d`;
}

function esc(t) {
  const d = document.createElement("span");
  d.textContent = t ?? "";
  return d.innerHTML;
}

// worktree lookup: path -> {branch, dirty, managedBy, repo}
function worktreeIndex(snap) {
  const idx = new Map();
  for (const r of snap.repos || [])
    for (const wt of r.worktrees || [])
      idx.set(wt.path, { ...wt, repo: r.name });
  return idx;
}

function worktreeURL(wt) {
  const tpl = state.cfg.worktreeUrlTemplate;
  if (!tpl || !wt.branch) return null;
  const slug = wt.branch.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  return tpl.replaceAll("{branch}", wt.branch).replaceAll("{slug}", slug);
}

function toast(msg, ms = 3500) {
  const t = $("#toast");
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(t._timer);
  t._timer = setTimeout(() => { t.hidden = true; }, ms);
}

// ---------- rendering ----------

function render() {
  const snap = state.snapshot;
  const board = $("#board");
  if (!snap) return;

  const sessions = snap.sessions || [];
  const counts = { needs: 0, working: 0, done: 0 };
  for (const s of sessions) {
    const b = bucketOf(s);
    if (b === "needs") counts.needs++;
    else if (b === "working") counts.working++;
    else if (s.state === "done") counts.done++;
  }
  for (const el of document.querySelectorAll(".counter")) {
    el.querySelector(".n").textContent = counts[el.dataset.filter] ?? 0;
    el.classList.toggle("active", state.filter === el.dataset.filter);
  }

  const wtIdx = worktreeIndex(snap);
  const parts = [];
  for (const bucket of BUCKETS) {
    let members = sessions.filter((s) => bucketOf(s) === bucket.id);
    if (state.filter === "needs") members = bucket.id === "needs" ? members : [];
    if (state.filter === "working") members = bucket.id === "working" ? members : [];
    if (state.filter === "done") members = bucket.id === "done" ? members.filter((s) => s.state === "done") : [];
    if (!members.length) continue;

    const groups = groupByRepoWorktree(members);
    const inner = groups.map(({ repo, worktrees }) => `
      <div class="repo">
        <h3>${esc(repo)}</h3>
        ${worktrees.map(({ path, sessions: ss }) => wtBlock(path, ss, wtIdx)).join("")}
      </div>`).join("");

    if (bucket.collapsed) {
      parts.push(`<details class="terminal"><summary>${bucket.title} (${members.length})</summary>${inner}</details>`);
    } else {
      parts.push(`<section class="bucket ${bucket.id}"><h2>${bucket.title}</h2>${inner}</section>`);
    }
  }
  board.innerHTML = parts.length ? parts.join("") : `<p class="empty">no sessions${state.filter ? " match the filter" : ""}</p>`;

  if (state.drawerKey) {
    const s = sessions.find((x) => x.key === state.drawerKey);
    if (s) fillDrawerMeta(s); else closeDrawer();
  }
}

function groupByRepoWorktree(sessions) {
  const repos = new Map();
  for (const s of sessions) {
    const repo = s.repo || "(loose)";
    if (!repos.has(repo)) repos.set(repo, new Map());
    const wts = repos.get(repo);
    const wt = s.worktree || "";
    if (!wts.has(wt)) wts.set(wt, []);
    wts.get(wt).push(s);
  }
  return [...repos.entries()].map(([repo, wts]) => ({
    repo,
    worktrees: [...wts.entries()].map(([path, sessions]) => ({ path, sessions })),
  }));
}

function wtBlock(path, sessions, wtIdx) {
  const wt = wtIdx.get(path);
  let head = "";
  if (wt) {
    const url = worktreeURL(wt);
    head = `<div class="wt-head">
      <span class="branch">⎇ ${esc(wt.branch || path.split("/").pop())}</span>
      ${wt.dirty === "true" ? '<span class="dirty" title="uncommitted changes">●</span>' : ""}
      ${wt.managedBy === "claude-code" ? '<span class="managed" title="created by Claude Code bg isolation — deleting the session may delete this worktree">claude-managed</span>' : ""}
      ${url ? `<a href="${esc(url)}" target="_blank" rel="noopener">open ↗</a>` : ""}
    </div>`;
  }
  return `<div class="wt">${head}${sessions.map(rowHTML).join("")}</div>`;
}

function rowHTML(s) {
  const cls = chipClass(s);
  const name = s.name || (s.kind === "tmux-interactive" ? "(interactive in tmux)" : "(unnamed)");
  const branchPart = s.worktree ? s.worktree.split("/").pop() : "";
  return `<button class="row ${cls}" data-key="${esc(s.key)}">
    <span class="chip ${cls}"></span>
    <span class="body">
      <div class="name">${esc(name)}</div>
      <div class="ctx">${esc(s.repo || "(loose)")}${branchPart ? " · " + esc(branchPart) : ""}</div>
      ${s.waitingFor ? `<div class="waiting">waiting: ${esc(s.waitingFor)}</div>` : ""}
    </span>
    <span class="meta">
      ${age(s)}
      ${s.tmuxTarget ? `<div class="badge">⧉ ${esc(s.tmuxTarget)}</div>` : ""}
    </span>
  </button>`;
}

// ---------- drawer ----------

function openDrawer(key) {
  state.drawerKey = key;
  const s = (state.snapshot?.sessions || []).find((x) => x.key === key);
  if (!s) return;
  fillDrawerMeta(s);
  $("#drawer").hidden = false;
  $("#drawer-backdrop").hidden = false;
  loadLogs();
}

function closeDrawer() {
  state.drawerKey = null;
  $("#drawer").hidden = true;
  $("#drawer-backdrop").hidden = true;
}

function fillDrawerMeta(s) {
  $("#drawer-chip").innerHTML = `<span class="chip ${chipClass(s)}" style="display:inline-block"></span>`;
  $("#drawer-title").textContent = s.name || s.key;
  const rows = [
    ["state", s.state + (s.waitingFor ? ` · ${s.waitingFor}` : "")],
    ["kind", s.kind],
    ["repo", s.repo || "—"],
    ["worktree", s.worktree || "—"],
    ["cwd", s.cwd || "—"],
    ["age", age(s) || "—"],
    ["tmux", s.tmuxTarget || "—"],
    ["id", s.id || "—"],
    ["sessionId", s.sessionId || "—"],
  ];
  $("#drawer-meta").innerHTML = rows.map(([k, v]) => `<dt>${k}</dt><dd>${esc(v)}</dd>`).join("");

  const copies = [];
  if (s.id) copies.push({ label: "copy attach cmd", text: `claude attach ${s.id}` });
  if (s.tmuxTarget) copies.push({ label: "copy tmux attach", text: `ssh ${location.hostname} -t tmux attach -t ${s.tmuxTarget.split(":")[0]}` });
  if (s.sessionId) copies.push({ label: "copy resume cmd", text: `claude --resume ${s.sessionId}` });
  $("#drawer-copy").innerHTML = "";
  for (const c of copies) {
    const b = document.createElement("button");
    b.textContent = c.label;
    b.onclick = () => navigator.clipboard.writeText(c.text).then(() => toast(`copied: ${c.text}`));
    $("#drawer-copy").appendChild(b);
  }

  renderDrawerActions(s);
}

async function loadLogs() {
  const key = state.drawerKey;
  if (!key) return;
  $("#drawer-logs").textContent = "loading…";
  try {
    const r = await fetch(`/api/v1/sessions/${encodeURIComponent(key)}/logs?lines=200`);
    const text = await r.text();
    $("#drawer-logs").textContent = r.ok ? (text.trim().slice(-16000) || "(empty)") : text;
    $("#drawer-logs").scrollTop = $("#drawer-logs").scrollHeight;
  } catch (e) {
    $("#drawer-logs").textContent = `failed: ${e}`;
  }
}

// ---------- actions (M5, rendered only when enabled) ----------

async function act(path, body, confirmMsg) {
  if (confirmMsg && !window.confirm(confirmMsg)) return null;
  try {
    const r = await fetch(path, {
      method: "POST",
      headers: { "X-Clauditor-Action": "1", "Content-Type": "application/json" },
      body: JSON.stringify(body ?? {}),
    });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) {
      toast(`✗ ${data.error?.message || r.status}`);
      return null;
    }
    return data;
  } catch (e) {
    toast(`✗ ${e}`);
    return null;
  }
}

function renderDrawerActions(s) {
  const box = $("#drawer-actions");
  box.innerHTML = "";
  box.hidden = !state.cfg.actionsEnabled;
  if (!state.cfg.actionsEnabled) return;
  const key = encodeURIComponent(s.key);

  if (s.id) {
    const open = document.createElement("button");
    open.textContent = "open in tmux";
    open.onclick = async () => {
      const res = await act(`/api/v1/sessions/${key}/open-in-tmux`);
      if (res) toast(`attached in ${res.target} — ${res.attach}`);
    };
    box.appendChild(open);

    const reply = document.createElement("button");
    reply.textContent = state.cfg.experimentalReply ? "reply (experimental)" : "reply → open in tmux";
    reply.onclick = async () => {
      if (!state.cfg.experimentalReply) {
        const res = await act(`/api/v1/sessions/${key}/open-in-tmux`);
        if (res) toast(`reply from a real terminal: ${res.attach} (window ${res.target})`);
        return;
      }
      const text = window.prompt("reply text (single digit for numbered choices):");
      if (!text) return;
      const res = await act(`/api/v1/sessions/${key}/reply`, { text });
      if (res) toast("✓ delivered");
    };
    box.appendChild(reply);

    if (s.state === "stopped" || s.state === "failed") {
      const respawn = document.createElement("button");
      respawn.textContent = "respawn";
      respawn.onclick = async () => {
        if (await act(`/api/v1/sessions/${key}/respawn`)) toast("✓ respawned");
      };
      box.appendChild(respawn);
    }
    if (s.state === "working" || s.state === "blocked") {
      const stop = document.createElement("button");
      stop.className = "danger";
      stop.textContent = "stop";
      stop.onclick = async () => {
        if (await act(`/api/v1/sessions/${key}/stop`, null, `Stop "${s.name || s.id}"? The conversation is kept; resume later with claude attach.`)) {
          toast("✓ stopped");
          closeDrawer();
        }
      };
      box.appendChild(stop);
    }
  }
}

// ---------- dispatch sheet (M5) ----------

function workingCount() {
  return (state.snapshot?.sessions || []).filter((s) => s.state === "working").length;
}

function openSheet() {
  fillRepoPicker();
  $("#sheet-working").textContent = workingCount();
  $("#sheet").hidden = false;
  $("#sheet-backdrop").hidden = false;
}

function closeSheet() {
  $("#sheet").hidden = true;
  $("#sheet-backdrop").hidden = true;
}

function fillRepoPicker() {
  const repos = (state.snapshot?.repos || []).filter((r) => r.name !== "(loose)");
  $("#d-repo").innerHTML = repos.map((r) => `<option value="${esc(r.name)}">${esc(r.name)}</option>`).join("");
  fillWorktreePicker();
}

function fillWorktreePicker() {
  const repo = (state.snapshot?.repos || []).find((r) => r.name === $("#d-repo").value);
  const opts = (repo?.worktrees || []).map((wt) =>
    `<option value="${esc(wt.path)}">${esc(wt.branch || wt.path.split("/").pop())}${wt.dirty === "true" ? " ●" : ""}</option>`);
  opts.push(`<option value="__new__">+ new worktree…</option>`);
  $("#d-worktree").innerHTML = opts.join("");
  $("#d-new-wt").hidden = $("#d-worktree").value !== "__new__";
}

async function submitDispatch(e) {
  e.preventDefault();
  const repo = $("#d-repo").value;
  const wt = $("#d-worktree").value;
  const target = { repo };
  if (wt === "__new__") {
    const branch = $("#d-branch").value.trim();
    if (!branch) { toast("branch name required for a new worktree"); return; }
    target.newWorktree = { branch, base: $("#d-base").value.trim() || undefined };
  } else if (wt) {
    target.worktree = wt;
  }
  const body = {
    target,
    prompt: $("#d-prompt").value,
    name: $("#d-name").value.trim() || undefined,
    model: $("#d-model").value.trim() || undefined,
  };
  $("#d-submit").disabled = true;
  const res = await act("/api/v1/dispatch", body);
  $("#d-submit").disabled = false;
  if (!res) return;
  closeSheet();
  $("#d-prompt").value = "";
  const where = res.createdWorktree ? ` in new worktree ${res.createdWorktree}` : "";
  toast(`⏳ dispatched${where} — waiting for it to appear…`, 6000);
  if (res.shortId) watchForSession(res.shortId);
}

// watchForSession toasts once the dispatched session shows up in a snapshot.
function watchForSession(shortId) {
  const started = Date.now();
  const t = setInterval(() => {
    const found = (state.snapshot?.sessions || []).find((s) => s.id === shortId);
    if (found) {
      clearInterval(t);
      toast(`✓ session ${shortId} is ${found.state} — ${found.name || ""}`);
    } else if (Date.now() - started > 30000) {
      clearInterval(t);
      toast(`session ${shortId} not visible yet — check the board`);
    }
  }, 1500);
}

// ---------- live updates ----------

function connectSSE() {
  const es = new EventSource("/api/v1/events");
  es.addEventListener("snapshot", (ev) => {
    state.snapshot = JSON.parse(ev.data);
    state.staleSince = null;
    $("#stale").hidden = true;
    $("#fab-working").textContent = `· ${workingCount()} working`;
    render();
  });
  es.onerror = () => {
    if (!state.staleSince) state.staleSince = Date.now();
    $("#stale").hidden = false;
    // EventSource auto-reconnects; the banner just tells the truth meanwhile.
  };
  setInterval(() => {
    if (state.staleSince) $("#stale-secs").textContent = Math.round((Date.now() - state.staleSince) / 1000);
  }, 1000);
}

// ---------- boot ----------

async function boot() {
  try {
    const r = await fetch("/api/v1/config");
    if (r.ok) state.cfg = await r.json();
  } catch { /* read-only defaults */ }

  try {
    const r = await fetch("/api/v1/state");
    if (r.ok) { state.snapshot = await r.json(); render(); }
  } catch { /* SSE will fill in */ }

  connectSSE();

  document.body.addEventListener("click", (e) => {
    const row = e.target.closest(".row");
    if (row) openDrawer(row.dataset.key);
    const counter = e.target.closest(".counter");
    if (counter) {
      state.filter = state.filter === counter.dataset.filter ? null : counter.dataset.filter;
      render();
    }
  });
  $("#drawer-close").onclick = closeDrawer;
  $("#drawer-backdrop").onclick = closeDrawer;
  $("#logs-refresh").onclick = loadLogs;

  // dispatch sheet (rendered only when actions are enabled)
  if (state.cfg.actionsEnabled) {
    $("#fab").hidden = false;
    $("#fab-working").textContent = `· ${workingCount()} working`;
    $("#fab").onclick = openSheet;
    $("#sheet-close").onclick = closeSheet;
    $("#sheet-backdrop").onclick = closeSheet;
    $("#d-repo").onchange = fillWorktreePicker;
    $("#d-worktree").onchange = () => { $("#d-new-wt").hidden = $("#d-worktree").value !== "__new__"; };
    $("#dispatch-form").onsubmit = submitDispatch;
  }
}

boot();
