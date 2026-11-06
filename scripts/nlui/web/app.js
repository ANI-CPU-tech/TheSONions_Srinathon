'use strict';
// NineLives Lab page. It talks only to the nlui server it came from, with
// the per-launch token from the URL. Everything the server sends goes into
// the page as text (textContent), never as HTML.

// ---------- helpers ----------

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => Array.from(document.querySelectorAll(sel));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;

// h builds an element: props are attributes, "class", "text" or on<event>.
function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'text') el.textContent = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else el.setAttribute(k, v === true ? '' : String(v));
  }
  for (const kid of kids.flat()) {
    if (kid != null && kid !== false) el.append(kid instanceof Node ? kid : String(kid));
  }
  return el;
}

function svgEl(tag, attrs) {
  const el = document.createElementNS('http://www.w3.org/2000/svg', tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
  return el;
}

function fmtB(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / 1048576).toFixed(1)} MiB`;
}

const basename = (p) => p.slice(p.lastIndexOf('/') + 1);
const num = (sel) => Math.max(0, Math.floor(Number($(sel).value) || 0));

// ---------- token and API ----------

const TOKEN = readToken();

// readToken takes the token from #t=..., keeps it for reloads of this tab
// and removes it from the address bar.
function readToken() {
  const m = location.hash.match(/t=([0-9a-f]{32})/);
  let t = m ? m[1] : '';
  try {
    if (t) sessionStorage.setItem('nl-token', t);
    else t = sessionStorage.getItem('nl-token') || '';
  } catch { /* storage blocked: the token works for this load only */ }
  if (m) history.replaceState(null, '', location.pathname);
  return t;
}

// A 403 means this page's token is not the server's: nlui was restarted.
function checkToken(res) {
  if (res.status === 403) banner('This page is from an earlier nlui run (its session token is stale). Open the new URL nlui printed in its terminal.');
}

// A new #t= (nlui restarted, same window) needs a fresh page.
window.addEventListener('hashchange', () => { if (/t=[0-9a-f]{32}/.test(location.hash)) location.reload(); });

async function api(path, body) {
  const opts = { method: body === undefined ? 'GET' : 'POST', headers: { 'X-NL-Token': TOKEN } };
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  checkToken(res);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `${res.status} ${res.statusText}`);
  return data;
}

// streamPost reads an NDJSON response and calls onEvent per line; with
// body null it is a GET.
async function streamPost(path, body, onEvent, signal) {
  const opts = { method: 'GET', signal, headers: { 'X-NL-Token': TOKEN } };
  if (body !== null) {
    Object.assign(opts, { method: 'POST', body: JSON.stringify(body) });
    opts.headers['Content-Type'] = 'application/json';
  }
  const res = await fetch(path, opts);
  checkToken(res);
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || `${res.status} ${res.statusText}`);
  }
  const reader = res.body.getReader();
  const dec = new TextDecoder();
  let buf = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let i;
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i);
      buf = buf.slice(i + 1);
      if (line) onEvent(JSON.parse(line));
    }
  }
}

// ---------- state, status, busy ----------

const S = { model: null, paths: {}, running: null, job: null, serve: null, state: null };
const CLI_ACTS = new Set(['open', 'put', 'get', 'del', 'batch', 'dump', 'compact', 'recover']);
const TORTURE_ACTS = new Set(['torture', 'torture-all', 'selftest', 'clean-clone']);

function setRunning(r) {
  S.running = r;
  const pill = $('#status');
  pill.className = 'pill' + (r ? ' run' : '');
  pill.textContent = r ? r.title : 'idle';
  $('#stop').hidden = !r;
  for (const b of $$('[data-act]')) b.disabled = !!r;
  serveControls();
}

function setResult(ok, text) {
  const pill = $('#status');
  pill.className = 'pill ' + (ok ? 'ok' : 'bad');
  pill.textContent = (ok ? '✓ ' : '✗ ') + text;
}

function banner(text, action) {
  const b = $('#banner');
  b.replaceChildren(text);
  if (action) b.append(' ', h('button', { class: 'btn', type: 'button', text: action.label, onclick: action.run }));
  b.hidden = !text;
}

async function refreshState(withDisk) {
  try {
    S.state = await api('/api/state');
  } catch (e) {
    banner('Cannot reach nlui: ' + e.message);
    return;
  }
  $('#dbpath').textContent = S.state.db;
  $('#dbpath').title = S.state.db;
  const bins = $('#bins');
  bins.replaceChildren(...S.state.bins.map((b) => h('span', {
    class: 'bin ' + (b.built ? 'ok' : 'missing'), text: b.path.replace('./bin/', ''),
    title: b.built ? `${b.path} built ${b.when}` : `${b.path} is not built`,
  })));
  const nl = S.state.bins.find((b) => b.path === './bin/ninelives');
  if (nl && !nl.built) banner('bin/ninelives is not built yet, so nothing can run.', { label: 'build now', run: () => runAction('build') });
  else banner('');
  if (withDisk && !S.running) renderDisk(S.state.snapshot, null);
}

// ---------- console ----------

const con = { el: null, queue: [], scheduled: false, blocks: 0 };
const MAX_BLOCK_LINES = 4000;
const MAX_BLOCKS = 400;

function conAppend(parent, node) {
  con.queue.push([parent, node]);
  if (!con.scheduled) {
    con.scheduled = true;
    requestAnimationFrame(conFlush);
  }
}

// conFlush adds queued lines once per frame and keeps the console bounded.
function conFlush() {
  con.scheduled = false;
  const el = con.el;
  const stick = el.scrollTop + el.clientHeight >= el.scrollHeight - 40;
  const touched = new Set();
  for (const [parent, node] of con.queue) {
    parent.append(node);
    if (parent === el) con.blocks++;
    else touched.add(parent);
  }
  con.queue = [];
  for (const block of touched) trimBlock(block);
  while (con.blocks > MAX_BLOCKS && el.firstChild) {
    el.firstChild.remove();
    con.blocks--;
  }
  if (stick) el.scrollTop = el.scrollHeight;
}

function trimBlock(block) {
  const lines = block.querySelectorAll(':scope > .cline');
  if (lines.length <= MAX_BLOCK_LINES) return;
  for (let i = 0; i < lines.length - MAX_BLOCK_LINES; i++) lines[i].remove();
  if (!block.querySelector(':scope > .cut')) {
    block.insertBefore(h('div', { class: 'cline note cut', text: '… earlier lines dropped from view' }), block.querySelector(':scope > .cline'));
  }
}

function conJob(title) { conAppend(con.el, h('div', { class: 'cjob', text: title })); }

function conBlock(title, cmd) {
  const block = h('div', { class: 'cblock' });
  if (title) block.append(h('div', { class: 'ctitle', text: title }));
  if (cmd) block.append(cmdLine(cmd));
  conAppend(con.el, block);
  return block;
}

function cmdLine(cmd) {
  const el = h('div', { class: 'ccmd', text: cmd, role: 'button', tabindex: '0', title: 'copy this command' });
  el.addEventListener('click', () => copyText(cmd, el));
  el.addEventListener('keydown', (e) => { if (e.key === 'Enter') copyText(cmd, el); });
  return el;
}

async function copyText(text, el) {
  try {
    await navigator.clipboard.writeText(text);
    el.classList.add('copied');
    setTimeout(() => el.classList.remove('copied'), 1200);
  } catch { /* clipboard refused: the text is still selectable */ }
}

const conLine = (block, stream, text) => conAppend(block, h('div', { class: 'cline ' + stream, text }));
const conError = (msg) => conAppend(con.el, h('div', { class: 'cerr', text: '✗ ' + msg }));

function exitText(ex) {
  let t = ex.signal ? `killed by ${ex.signal} (exit ${ex.code})` : `exit ${ex.code}`;
  if (ex.err) t += ` · ${ex.err}`;
  return `${ex.ok ? '✓' : '✗'} ${t} · ${ex.ms} ms${ex.ok && ex.code !== 0 ? ' · expected' : ''}`;
}

const conExit = (block, ex) => conAppend(block, h('div', { class: 'cexit ' + (ex.ok ? 'ok' : 'bad'), text: exitText(ex) }));

// ---------- jobs ----------

// runAction runs one catalogue action and resolves with the finished job,
// or null if another run holds the database.
async function runAction(action, args = [], maxBytes = 0) {
  if (S.running) return null;
  const ac = new AbortController();
  const job = { action, title: action, steps: [], ok: false, error: '' };
  S.job = job;
  setRunning({ kind: 'job', title: [action, ...args].join(' ').slice(0, 80), abort: ac });
  playerReset();
  jobCardReset();
  try {
    await streamPost('/api/run', { action, args, maxBytes }, (ev) => onJobEvent(job, ev), ac.signal);
  } catch (e) {
    job.error = e.name === 'AbortError' ? 'stopped' : e.message;
    conError(`${action}: ${job.error}`);
  } finally {
    setRunning(null);
    setResult(job.ok, job.error || job.title);
    refreshState(false);
  }
  return job;
}

function onJobEvent(job, ev) {
  switch (ev.type) {
    case 'job':
      job.title = ev.title;
      job.count = ev.count;
      conJob(ev.title);
      break;
    case 'step': {
      if (ev.step > 1) finishStep(job, ev.step - 1);
      const st = { n: ev.step, title: ev.title, cmd: ev.cmd, out: [], block: conBlock(ev.title, ev.cmd) };
      job.steps[ev.step - 1] = st;
      jobCardStep(st);
      break;
    }
    case 'out': {
      const st = job.steps[ev.step - 1];
      if (st.out.length < 400) st.out.push(ev.text);
      conLine(st.block, ev.stream, ev.text);
      break;
    }
    case 'exit': {
      const st = job.steps[ev.step - 1];
      st.exit = ev.exit;
      conExit(st.block, ev.exit);
      jobCardStep(st);
      if (TORTURE_ACTS.has(job.action)) tortureExit(ev.exit);
      break;
    }
    case 'snapshot':
      if (ev.step) Object.assign(job.steps[ev.step - 1], { snapshot: ev.snapshot, diff: ev.diff });
      else if (!job.steps.length) renderDisk(ev.snapshot, null);
      break;
    case 'trace':
      job.steps[ev.step - 1].trace = ev.trace;
      finishStep(job, ev.step);
      break;
    case 'torture':
      onTorture(ev.torture, job.count > 1 ? job.steps[ev.step - 1] : null);
      break;
    case 'done':
      if (job.steps.length) finishStep(job, job.steps.length);
      job.ok = ev.ok;
      if (!ev.ok) job.error = `${job.title}: ${ev.text}`;
      $('#job-sum').textContent = ev.ok ? '✓ ' + ev.text : '✗ ' + ev.text;
      break;
  }
}

// finishStep hands a complete step to the player, once.
function finishStep(job, n) {
  const st = job.steps[n - 1];
  if (!st || st.queued || !(st.snapshot || st.trace)) return;
  st.queued = true;
  addChip(st);
  P.queue.push(st);
  pump();
}

// ---------- job card (Build + tests) ----------

function jobCardReset() {
  $('#jobsteps').replaceChildren();
  $('#job-sum').textContent = '';
}

function jobCardStep(st) {
  const id = 'js-' + st.n;
  let li = document.getElementById(id);
  const state = !st.exit ? 'run' : st.exit.ok ? 'ok' : 'bad';
  const icon = { run: '…', ok: '✓', bad: '✗' }[state];
  const row = [
    h('span', { class: 'ic', text: icon, 'aria-label': state }),
    h('div', {}, h('div', { text: st.title || st.cmd }), h('code', { text: st.cmd })),
    h('span', { class: 'ex', text: st.exit ? exitText(st.exit).slice(2) : 'running' }),
  ];
  if (!li) {
    li = h('li', { id });
    $('#jobsteps').append(li);
  }
  li.className = state;
  li.replaceChildren(...row);
}

// ---------- player: replays a job step by step ----------

const P = { queue: [], busy: false, token: 0, current: null };

function playerReset() {
  P.queue = [];
  P.token++;
  P.current = null;
  $('#steppick').replaceChildren();
  $('#replay').disabled = true;
}

async function pump() {
  if (P.busy) return;
  P.busy = true;
  while (P.queue.length) {
    await playStep(P.queue.shift());
    if (speed()) await sleep(300);
  }
  P.busy = false;
}

async function playStep(st) {
  const my = ++P.token;
  P.current = st;
  $('#replay').disabled = false;
  for (const c of $$('#steppick .chip')) c.setAttribute('aria-selected', String(c.dataset.n === String(st.n)));
  if (st.snapshot) renderDisk(st.snapshot, st.diff);
  await animateTrace(st.trace, my, st.title || st.cmd);
}

function addChip(st) {
  const killed = st.exit && st.exit.signal === 'SIGKILL';
  const cls = !st.exit ? '' : killed ? 'killed' : st.exit.ok ? 'ok' : 'bad';
  const label = `${st.n}. ${(st.title || st.cmd).replace(/^\.\/bin\/ninelives /, '').slice(0, 42)}`;
  const chip = h('button', { type: 'button', class: 'chip ' + cls, role: 'option', 'aria-selected': 'false', text: label, title: st.cmd });
  chip.dataset.n = String(st.n);
  chip.addEventListener('click', () => { P.queue = []; playStep(st); });
  $('#steppick').append(chip);
}

const speed = () => (reduceMotion ? 0 : Number($('#speed').value));

// ---------- code path ----------

// nodeStates says how each node of a path ended in one segment.
function nodeStates(seg, path) {
  const stop = seg.stop ? path.nodes.findIndex((n) => n.id === seg.stop) : -1;
  const lit = new Set(seg.lit || []);
  const skip = new Set(seg.skip || []);
  return path.nodes.map((n, i) => {
    if (n.branch && !lit.has(n.id)) return 'off';
    if (seg.status === 'killed' && !seg.stop) return 'notrun'; // killed from outside: where is unknown
    if (seg.status === 'corrupt' && n.id === 'verdict') return 'done';
    if (stop >= 0 && i > stop) return 'notrun';
    if (i === stop) return n.crash ? 'killed' : seg.status;
    if (n.crash) return 'passed';
    return skip.has(n.id) ? 'skipped' : 'done';
  });
}

const STATE_WORD = {
  done: 'done', off: 'not taken', skipped: 'not run', notrun: 'not reached', passed: 'passed',
  killed: 'killed here', refused: 'refused here', failed: 'failed here', corrupt: 'problem found here',
};

function nodeEl(n) {
  const where = [n.where.join(' · '), n.spec].filter(Boolean).join('    ');
  return h('li', { class: 'node pending' + (n.crash ? ' crash' : '') },
    h('span', { class: 'mark', 'aria-hidden': 'true' }),
    h('div', {},
      h('div', { class: 'ntitle', text: n.crash ? n.id : n.title }),
      h('div', { class: 'nwhere', text: where }),
      n.note ? h('div', { class: 'nnote', text: n.note }) : null,
      h('span', { class: 'sr', text: '' })));
}

function worst(trace) {
  for (const s of ['killed', 'failed', 'refused', 'corrupt', 'missed']) {
    if (trace.segments.some((g) => g.status === s)) return s;
  }
  return 'done';
}

// animateTrace draws the trace and lights its nodes one by one. A newer
// call (higher P.token) stops an older one.
async function animateTrace(trace, my, label) {
  const box = $('#trace');
  const sum = $('#tracesum');
  box.replaceChildren();
  if (!trace) {
    sum.className = 'tracesum';
    sum.textContent = `${label}: an in-process step (no engine code path)`;
    return;
  }
  sum.className = 'tracesum ' + worst(trace);
  sum.textContent = trace.summary;
  const plan = [];
  for (const seg of trace.segments) {
    const path = S.paths[seg.path];
    if (!path) continue;
    const badge = h('span', { class: 'badge', text: '…' });
    const list = h('ol', { class: 'nodes' });
    const els = path.nodes.map(nodeEl);
    list.append(...els);
    box.append(h('div', { class: 'seg' },
      h('div', { class: 'seghead' }, h('strong', { text: path.title }), badge),
      seg.note ? h('p', { class: 'segnote', text: seg.note }) : null, list));
    plan.push({ seg, badge, els, states: nodeStates(seg, path), nodes: path.nodes });
  }
  for (const p of plan) {
    for (let i = 0; i < p.els.length; i++) {
      if (my !== P.token) return;
      const el = p.els[i];
      const state = p.states[i];
      const slow = speed() && !p.nodes[i].crash && state !== 'off';
      if (slow) {
        el.className = el.className.replace('pending', 'active');
        await sleep(speed());
        if (my !== P.token) return;
      }
      el.className = `node ${state}${p.nodes[i].crash ? ' crash' : ''}`;
      el.querySelector('.sr').textContent = STATE_WORD[state] || state;
      if (state === 'killed' || state === 'refused' || state === 'failed') el.scrollIntoView({ block: 'nearest' });
    }
    p.badge.className = 'badge ' + p.seg.status;
    p.badge.textContent = p.seg.status;
  }
}

// ---------- disk ----------

const disk = { snap: null, sel: null, hexToken: 0 };

function renderDisk(snap, diff) {
  if (!snap) return;
  disk.snap = snap;
  const newFrom = diff && diff.added > 0 ? snap.records.length - diff.added : Infinity;
  renderDiskSummary(snap);
  renderFileBar(snap, newFrom);
  renderDiff(diff, snap);
  renderRecords(snap, newFrom);
  renderIndex(snap);
  if (disk.sel && !selectionValid(snap, disk.sel)) disk.sel = null;
  renderHex();
}

function stat(cls, ...parts) { return h('span', { class: 'stat ' + cls }, ...parts); }

function renderDiskSummary(s) {
  $('#disk-title').textContent = 'Disk: ' + basename(s.path);
  $('#disk-when').textContent = 'as of ' + new Date().toLocaleTimeString();
  const el = $('#disksum');
  el.replaceChildren();
  if (s.error) return el.append(stat('bad', 'cannot read: ' + s.error));
  if (!s.exists) el.append(stat('', 'no file yet: the next command creates it'));
  else {
    el.append(stat('', h('b', { text: fmtB(s.size) }), ' file'),
      stat('', h('b', { text: String(s.records.length) }), ' records'),
      stat('', h('b', { text: String(s.keys) }), ' keys'),
      stat('', 'live ', h('b', { text: fmtB(s.liveBytes) })),
      stat('', 'dead ', h('b', { text: fmtB(s.deadBytes) })),
      stat('', 'next seq ', h('b', { text: String(s.nextSeq) })));
  }
  for (const t of s.tempFiles) el.append(stat('warn', 'temp file: ' + t));
  if (s.headerErr) el.append(stat('bad', 'bad header: ' + s.headerErr));
  if (s.torn) el.append(stat('bad', `torn tail: ${s.torn.bytes} B at offset ${s.torn.off}`));
  if (s.corrupt) el.append(stat('bad', 'corrupt: ' + s.corrupt));
  if (s.truncated) el.append(stat('warn', 'large file: the view shows the first part'));
}

function renderFileBar(s, newFrom) {
  const bar = $('#filebar');
  bar.replaceChildren();
  if (!s.exists || s.size === 0) {
    bar.append(h('div', { class: 'empty', text: s.exists ? 'empty file' : 'no file' }));
    return;
  }
  const dense = s.records.length > 150;
  const add = (cls, bytes, title, sel) => {
    const seg = h('div', { class: 'seg-b ' + cls, title, 'aria-label': title });
    seg.style.flexGrow = String(Math.max(bytes, 1));
    seg.style.flexBasis = '0';
    if (dense) seg.style.minWidth = '1px';
    seg.addEventListener('click', () => select(sel));
    seg.dataset.sel = selKey(sel);
    bar.append(seg);
  };
  if (s.headerErr) return add('b-torn', s.size, 'not a NineLives header: ' + s.headerErr, { kind: 'raw' });
  add('b-hdr', 24, 'file header · 24 B', { kind: 'hdr' });
  s.records.forEach((r, i) => {
    const cls = `b-${r.type}${r.live ? '' : ' b-dead'}${i >= newFrom ? ' new' : ''}`;
    add(cls, r.size, `${r.type} seq ${r.seq} · ${r.size} B at ${r.off}${r.live ? '' : ' · dead'}`, { kind: 'rec', i });
  });
  if (s.torn) add('b-torn', s.torn.bytes, `torn tail · ${s.torn.bytes} B at ${s.torn.off}: ${s.torn.reason}`, { kind: 'torn' });
  markSelection();
}

function renderDiff(d, snap) {
  const el = $('#diff');
  if (!d) return (el.textContent = '');
  const parts = [];
  if (d.created) parts.push('file created');
  if (d.removed) parts.push('file removed');
  if (d.rewritten) parts.push(`file rewritten (compaction): ${fmtB(d.sizeFrom)} → ${fmtB(d.sizeTo)}`);
  else if (d.added) parts.push(`+${d.added} record${d.added > 1 ? 's' : ''} (${fmtB(d.sizeTo - d.sizeFrom)})`);
  if (d.tornCut) parts.push(`recovery cut a ${d.tornCut} B torn tail`);
  else if (snap.torn && !d.rewritten && snap.torn.off >= d.sizeFrom) parts.push(`a partial record of ${snap.torn.bytes} B appeared (torn tail)`);
  for (const t of d.tempNew || []) parts.push(`${t} appeared`);
  for (const t of d.tempGone || []) parts.push(`${t} deleted`);
  if (!d.changed) parts.push('file unchanged');
  else if (!parts.length && d.sizeTo !== d.sizeFrom) parts.push(`${fmtB(d.sizeFrom)} → ${fmtB(d.sizeTo)}`);
  el.textContent = parts.join(' · ');
}

function opsText(r) {
  const shown = r.ops.slice(0, 3).map((o) => (o.kind === 'put' ? `put ${o.key} ${o.value}` : `del ${o.key}`));
  return shown.join('; ') + (r.opCount > 3 ? `; +${r.opCount - 3} more` : '');
}

function recordState(r) {
  if (r.type === 'DEL') return 'delete marker (dead bytes)';
  if (!r.live) return 'dead: overwritten or deleted';
  if (r.type === 'BATCH') return `live (${r.ops.filter((o) => o.live).length} of ${r.opCount} ops)`;
  return 'live';
}

function renderRecords(s, newFrom) {
  const tb = $('#records tbody');
  const rows = [];
  const tr = (sel, cls, cells) => {
    const row = h('tr', { class: 'click ' + cls, tabindex: '0' }, ...cells.map((c) => h('td', {}, c)));
    row.dataset.sel = selKey(sel);
    row.addEventListener('click', () => select(sel));
    row.addEventListener('keydown', (e) => { if (e.key === 'Enter') select(sel); });
    rows.push(row);
  };
  if (s.header) tr({ kind: 'hdr' }, '', ['0', '24', h('span', { class: 't HDR', text: 'HDR' }), '–', `${s.header.magic} v${s.header.version} · SeqBase ${s.header.seqBase}`, 'file header']);
  s.records.forEach((r, i) => tr({ kind: 'rec', i }, `${r.live ? '' : 'dead'}${i >= newFrom ? ' new' : ''}`,
    [String(r.off), String(r.size), h('span', { class: 't ' + r.type, text: r.type }), String(r.seq), opsText(r), recordState(r)]));
  if (s.torn) tr({ kind: 'torn' }, 'tornrow', [String(s.torn.off), String(s.torn.bytes), 'TORN', '–', s.torn.reason, 'open() cuts this']);
  tb.replaceChildren(...rows);
  markSelection();
}

function renderIndex(s) {
  const rows = s.index.map((x) => {
    const row = h('tr', { class: 'click', tabindex: '0', title: 'show the record that holds this value' },
      h('td', { text: x.key }), h('td', { text: x.value }), h('td', { text: String(x.seq) }), h('td', { text: String(x.off) }));
    const i = s.records.findIndex((r) => r.off === x.recAt);
    if (i >= 0) row.addEventListener('click', () => select({ kind: 'rec', i }));
    return row;
  });
  if (!rows.length) rows.push(h('tr', {}, h('td', { colspan: '4', class: 'muted', text: 'no live keys' })));
  $('#index tbody').replaceChildren(...rows);
}

const selKey = (sel) => (sel.kind === 'rec' ? 'rec' + sel.i : sel.kind);

function selectionValid(s, sel) {
  if (sel.kind === 'rec') return sel.i < s.records.length;
  if (sel.kind === 'torn') return !!s.torn;
  if (sel.kind === 'hdr') return !!s.header;
  return !!s.headerErr;
}

function select(sel) {
  disk.sel = sel;
  markSelection();
  renderHex();
}

function markSelection() {
  const key = disk.sel ? selKey(disk.sel) : '';
  for (const el of $$('#filebar .seg-b, #records tbody tr')) el.classList.toggle('sel', el.dataset.sel === key);
}

// ---------- hex inspector ----------

function headerFields() {
  return [
    { from: 0, to: 4, cls: 'f-key', name: 'magic NLV1' }, { from: 4, to: 6, cls: 'f-type', name: 'version' },
    { from: 6, to: 8, cls: 'f-meta', name: 'flags' }, { from: 8, to: 16, cls: 'f-seq', name: 'SeqBase' },
    { from: 16, to: 20, cls: 'f-meta', name: 'reserved' }, { from: 20, to: 24, cls: 'f-crc', name: 'CRC32-C of bytes 0-19' },
  ];
}

function recordFields(r) {
  const o = r.off;
  const f = [
    { from: o, to: o + 4, cls: 'f-crc', name: 'CRC32-C' }, { from: o + 4, to: o + 8, cls: 'f-len', name: 'Length' },
    { from: o + 8, to: o + 9, cls: 'f-type', name: 'Type' }, { from: o + 9, to: o + 17, cls: 'f-seq', name: 'Seq' },
  ];
  if (r.type === 'BATCH') f.push({ from: o + 17, to: o + 21, cls: 'f-cnt', name: 'count' });
  for (const op of r.ops) {
    if (r.type === 'BATCH') f.push({ from: op.keyOff - 3, to: op.keyOff - 2, cls: 'f-op', name: 'op (1 put, 2 del)' });
    f.push({ from: op.keyOff - 2, to: op.keyOff, cls: 'f-meta', name: 'key_len / val_len' });
    f.push({ from: op.keyOff, to: op.keyOff + op.keyLen, cls: 'f-key', name: 'key' });
    if (op.kind === 'put') {
      f.push({ from: op.valOff - 4, to: op.valOff, cls: 'f-meta', name: 'key_len / val_len' });
      f.push({ from: op.valOff, to: op.valOff + op.valLen, cls: 'f-val', name: 'value' });
    }
  }
  return f;
}

async function renderHex() {
  const s = disk.snap;
  const sel = disk.sel;
  const title = $('#hex-title');
  $('#hexgrid').replaceChildren();
  $('#hexlegend').replaceChildren();
  if (!s || !sel) return (title.textContent = 'Click a record to see its bytes');
  let off = 0;
  let len = 24;
  let fields = headerFields();
  let name = 'file header';
  if (sel.kind === 'rec') {
    const r = s.records[sel.i];
    [off, len, fields, name] = [r.off, r.size, recordFields(r), `${r.type} record, seq ${r.seq}`];
  } else if (sel.kind === 'torn') {
    [off, len, name] = [s.torn.off, s.torn.bytes, 'torn tail (never acked; open() cuts it)'];
    fields = [{ from: off, to: off + len, cls: 'f-torn', name: 'torn bytes' }];
  } else if (sel.kind === 'raw') {
    [len, name, fields] = [s.size, 'whole file (bad header)', []];
  }
  const shown = Math.max(1, Math.min(len, 4096));
  const my = ++disk.hexToken;
  let hex = s.hex ? s.hex.slice(off * 2, (off + shown) * 2) : null;
  let live = '';
  if (hex === null) { // a big file: read it as it is now
    try {
      hex = (await api(`/api/bytes?off=${off}&len=${shown}`)).hex;
    } catch (e) {
      title.textContent = 'cannot read bytes: ' + e.message;
      return;
    }
    if (my !== disk.hexToken) return; // a newer selection won
    live = ' (read from the file as it is now)';
  }
  title.textContent = `${name}: ${len} bytes at offset ${off}${len > shown ? `, first ${shown} shown` : ''}${live}`;
  drawHex(off, hex, fields);
}

function drawHex(base, hex, fields) {
  const n = hex.length / 2;
  const cls = new Array(n).fill('f-pad');
  const names = new Map();
  for (const f of fields) {
    names.set(f.name, f.cls);
    for (let a = Math.max(f.from, base); a < Math.min(f.to, base + n); a++) cls[a - base] = f.cls;
  }
  $('#hexlegend').replaceChildren(...[...names].map(([nm, c]) => h('span', { class: c, text: nm })));
  const rows = [];
  for (let r = 0; r < n; r += 16) {
    const bytes = h('span', { class: 'hexbytes' });
    let ascii = '';
    for (let i = r; i < Math.min(r + 16, n); i++) {
      const b = parseInt(hex.substr(i * 2, 2), 16);
      bytes.append(h('span', { class: cls[i], text: hex.substr(i * 2, 2) }));
      ascii += b > 0x20 && b < 0x7f ? String.fromCharCode(b) : '·';
    }
    rows.push(h('div', { class: 'hexrow' }, h('span', { class: 'hexoff', text: String(base + r).padStart(6, '0') }), bytes, h('span', { class: 'hexasc', text: ascii })));
  }
  $('#hexgrid').replaceChildren(...rows);
}

// ---------- serve ----------

async function startServe() {
  if (S.running) return;
  const ac = new AbortController();
  S.serve = { rows: [], running: true, block: null, prev: null, history: [] };
  setRunning({ kind: 'serve', title: 'serve is running', abort: ac });
  playerReset();
  $('#serve-reqs tbody').replaceChildren();
  conJob('serve');
  let ended = null;
  try {
    await streamPost('/api/serve/start', { maxBytes: num('#serve-max') }, (ev) => { ended = onServeEvent(ev) || ended; }, ac.signal);
  } catch (e) {
    conError('serve: ' + (e.name === 'AbortError' ? 'stopped' : e.message));
  } finally {
    S.serve.running = false;
    setRunning(null);
    if (ended) setResult(ended.ok, ended.text);
    refreshState(false);
  }
}

// onServeEvent handles one event; at the end it returns how serve ended.
function onServeEvent(ev) {
  const sv = S.serve;
  switch (ev.type) {
    case 'step':
      sv.block = conBlock(ev.title, ev.cmd);
      $('#serve-line').focus();
      break;
    case 'out':
      conLine(sv.block, ev.stream, ev.text);
      break;
    case 'snapshot':
      renderDisk(ev.snapshot, clientDiff(sv.prev, ev.snapshot));
      sv.prev = ev.snapshot;
      break;
    case 'trace':
      showTrace(ev.trace);
      break;
    case 'sent':
      addServeRow(ev.req);
      break;
    case 'reply':
      updateServeRow(ev.req, ev.trace);
      break;
    case 'inflight':
      for (const r of ev.reqs) updateServeRow(r, ev.trace);
      showTrace(ev.trace);
      break;
    case 'exit':
      conExit(sv.block, ev.exit);
      if (ev.exit.signal === 'SIGKILL') return { ok: true, text: 'serve killed by SIGKILL: now recover' };
      return { ok: ev.exit.code === 0, text: `serve exited with code ${ev.exit.code}` };
  }
  return null;
}

// clientDiff finds the new records between two serve snapshots.
function clientDiff(a, b) {
  if (!a || !b.exists) return null;
  const rewritten = a.exists && a.inode !== b.inode;
  const added = rewritten ? b.records.length : b.records.filter((r) => r.off >= a.validEnd).length;
  return { changed: a.size !== b.size || rewritten, rewritten, added, sizeFrom: a.size, sizeTo: b.size };
}

function showTrace(trace) {
  P.queue = [];
  animateTrace(trace, ++P.token, trace.action);
}

function addServeRow(req) {
  const tr = h('tr', { class: 'click', tabindex: '0' });
  const row = { req, tr, trace: null };
  tr.addEventListener('click', () => row.trace && showTrace(row.trace));
  S.serve.rows.push(row);
  drawServeRow(row);
  const tb = $('#serve-reqs tbody');
  tb.append(tr);
  tr.scrollIntoView({ block: 'nearest' });
}

function updateServeRow(req, trace) {
  const row = S.serve.rows.find((r) => r.req.opid === req.opid && r.req.status === 'waiting');
  if (!row) return;
  row.req = req;
  row.trace = trace;
  drawServeRow(row);
  if (req.status !== 'inflight') showTrace(trace);
}

function drawServeRow(row) {
  const r = row.req;
  const lines = r.text.split('\n');
  const text = lines[0] + (lines.length > 1 ? `  (+${lines.length - 1} lines)` : '');
  const reply = r.status === 'waiting' ? 'waiting' : r.status === 'inflight' ? 'in flight at the kill: may or may not survive' : r.reply;
  row.tr.replaceChildren(h('td', { text: String(r.opid) }), h('td', { text, title: r.text }), h('td', {}, h('span', { class: 'st ' + r.status, text: reply })));
}

async function serveSend() {
  const ta = $('#serve-line');
  const text = ta.value.replace(/\s+$/, '');
  if (!text || !S.serve || !S.serve.running) return;
  try {
    await api('/api/serve/send', { text });
    S.serve.history.push(text);
    ta.value = '';
  } catch (e) {
    conError('send: ' + e.message);
  }
}

function serveControls() {
  const on = !!(S.serve && S.serve.running && S.running && S.running.kind === 'serve');
  for (const sel of ['#serve-kill', '#serve-eof', '#serve-line', '#serve-send']) $(sel).disabled = !on;
  for (const c of $$('#serve-templates .chip')) c.disabled = !on;
}

// ---------- watch mode ----------

const W = { ac: null };

// startWatch follows the demo database while commands run in a terminal.
async function startWatch() {
  const ac = new AbortController();
  W.ac = ac;
  const cmd = `./bin/ninelives put ${S.state ? S.state.db : 'demo.db'} a 1`;
  $('#watchcmd').textContent = cmd;
  $('#watchhint').hidden = false;
  try {
    await streamPost('/api/watch', null, onWatchEvent, ac.signal);
  } catch (e) {
    if (e.name !== 'AbortError') conError('watch: ' + e.message);
  }
  if (W.ac === ac && !ac.signal.aborted) { // the server ended it: nlui is quitting
    $('#watch').checked = false;
    stopWatch();
  }
}

function stopWatch() {
  if (W.ac) W.ac.abort();
  W.ac = null;
  $('#watchhint').hidden = true;
}

// onWatchEvent shows a change made outside nlui. nlui's own runs show
// their changes themselves, so watch events are ignored while one runs.
function onWatchEvent(ev) {
  if (S.running) return;
  renderDisk(ev.snapshot, ev.diff || null);
  if (!ev.trace) return; // the first event: the file as it is now
  showTrace(ev.trace);
  const block = conBlock('terminal command, seen by watch mode', null);
  conLine(block, 'note', [ev.trace.summary, $('#diff').textContent].filter(Boolean).join(' · '));
}

function wireWatch() {
  $('#watch').addEventListener('change', (e) => (e.target.checked ? startWatch() : stopWatch()));
  const copyCmd = () => copyText($('#watchcmd').textContent, $('#watchcmd'));
  $('#watchcmd').addEventListener('click', copyCmd);
  $('#watchcmd').addEventListener('keydown', (e) => { if (e.key === 'Enter') copyCmd(); });
}

// ---------- crash lab ----------

function crashNode(p) {
  for (const path of S.model.paths) {
    const n = path.nodes.find((x) => x.crash && x.id === p);
    if (n) return n;
  }
  return null;
}

function buildCrashList() {
  const groups = [
    ['Write path', S.model.crashPoints.filter((p) => !p.startsWith('compact_') && !p.startsWith('recovery_'))],
    ['Compaction', S.model.crashPoints.filter((p) => p.startsWith('compact_'))],
    ['Recovery (kill during open)', S.model.crashPoints.filter((p) => p.startsWith('recovery_'))],
    ['By hand', ['f4']],
  ];
  const box = $('#crash-list');
  box.replaceChildren(...groups.map(([title, points]) => h('div', { class: 'crashgroup' },
    h('h2', { text: title }),
    ...points.map((p) => {
      const n = p === 'f4' ? null : crashNode(p);
      return h('div', { class: 'crashitem', id: 'cr-' + p },
        h('span', { class: 'name', text: p === 'f4' ? 'F4: cut 1 byte off the end' : p }),
        h('span', { class: 'where', text: n ? n.where.join(' · ') : 'truncate -s -1 demo.db, then open' }),
        h('button', { class: 'btn', type: 'button', 'data-act': p === 'f4' ? 'f4' : 'crash', 'data-arg': p === 'f4' ? null : p, text: 'run' }),
        h('span', { class: 'res', 'aria-live': 'polite' }));
    }))));
}

function crashVerdict(job, p) {
  if (!job) return { ok: false, text: 'did not run: busy' };
  if (!job.steps.length) return { ok: false, text: job.error || 'did not run' };
  const killed = job.steps.some((s) => s.exit && s.exit.signal === 'SIGKILL');
  const out = job.steps[job.steps.length - 1].out.join('\n');
  const kept = p === 'f4' ? /^a 1$/m.test(out) && /^b 1$/m.test(out) : /^base 0$/m.test(out);
  const missing = [];
  if (p !== 'f4' && !killed) missing.push('no SIGKILL happened');
  if (!job.ok) missing.push(job.error || 'a step failed');
  if (!kept) missing.push('the committed key is gone');
  if (missing.length) return { ok: false, text: '✗ ' + missing.join('; ') };
  return { ok: true, text: p === 'f4' ? '✓ torn record cut; check OK; a and b kept' : `✓ SIGKILL at ${p}; check OK; base kept` };
}

async function runCrash(p) {
  const job = await runAction(p === 'f4' ? 'f4' : 'crash', p === 'f4' ? [] : [p]);
  const v = crashVerdict(job, p);
  const res = document.querySelector(`#cr-${p} .res`);
  res.className = 'res ' + (v.ok ? 'ok' : 'bad');
  res.textContent = v.text;
  return v.ok;
}

async function runCrashAll() {
  let pass = 0;
  const all = [...S.model.crashPoints, 'f4'];
  for (const p of all) {
    if (await runCrash(p)) pass++;
  }
  setResult(pass === all.length, `crash lab: ${pass}/${all.length} recovered`);
}

// ---------- torture ----------

const T = { rows: [], of: 0, sum: null, verdict: '', cycleToken: 0, drawn: false };
const STATIONS = [
  ['serve', 'start serve, READY'], ['writers', '2 writers'], ['kill', 'SIGKILL'], ['open', 'open = recovery'],
  ['check', 'check'], ['dump', 'dump'], ['pass', 'verify vs oracle'],
];

function tortureReset(tl) {
  T.rows = [];
  T.of = tl.of;
  T.sum = { sent: 0, acked: 0, errs: 0, inflight: 0 };
  $('#t-run').textContent = `seed ${tl.seed} · mode ${tl.mode} · oracle log ${tl.text}`;
  verdict('run', `running ${tl.mode}, seed ${tl.seed}: 0/${tl.of}`);
  $('#t-rows tbody').replaceChildren();
  drawTiles(null);
  drawCycle(null);
  scheduleCharts();
}

function verdict(kind, text) {
  const el = $('#t-verdict');
  T.verdict = kind;
  el.className = 'verdict ' + kind;
  el.replaceChildren(text);
}

function onTorture(tl, st) {
  switch (tl.kind) {
    case 'start':
      tortureReset(tl);
      if (st) $('#t-run').textContent = `${st.title} · ${$('#t-run').textContent}`;
      break;
    case 'iter':
      onIter(tl);
      break;
    case 'pass':
      verdict('pass', `PASS ${tl.iter}/${tl.of} iterations (seed ${tl.seed}, mode ${tl.mode}): every acked write survived every kill`);
      break;
    case 'fail':
      verdict('fail', `FAIL at iteration ${tl.iter}/${tl.of}: delay ${tl.delayMs} ms, sent ${tl.sent}, acked ${tl.acked}, in flight ${tl.inflight}`);
      break;
    case 'detail': {
      const el = $('#t-verdict');
      if (T.verdict === 'fail' && el.childNodes.length < 8) el.append('\n' + tl.text.slice(0, 300));
      break;
    }
    case 'repro': {
      const code = h('code', { text: tl.text, title: 'copy' });
      code.addEventListener('click', () => copyText(tl.text, code));
      $('#t-verdict').append(code);
      break;
    }
    case 'failed':
      $('#t-verdict').append(`\nFAILED after ${tl.iter} of ${tl.of} iterations passed (seed ${tl.seed}, mode ${tl.mode})`);
      break;
    case 'setup':
      verdict('fail', 'torture could not start: ' + tl.text);
      break;
  }
}

// tortureExit relabels a FAIL the step expected (the oracle self-test).
function tortureExit(ex) {
  if (T.verdict === 'fail' && ex.ok) {
    const el = $('#t-verdict');
    el.className = 'verdict pass';
    el.prepend('✓ expected: the oracle caught the injected bug.\n');
    T.verdict = 'pass';
  }
}

function onIter(tl) {
  if (!T.sum) T.sum = { sent: 0, acked: 0, errs: 0, inflight: 0 };
  T.rows.push(tl);
  if (T.rows.length > 2000) T.rows.shift();
  for (const k of ['sent', 'acked', 'errs', 'inflight']) T.sum[k] += tl[k] || 0;
  $('#t-bar').style.width = `${Math.min(100, (100 * tl.iter) / (tl.of || 1))}%`;
  verdict('run', `running: ${tl.iter}/${tl.of} iterations passed`);
  drawTiles(tl);
  drawCycle(tl);
  const tb = $('#t-rows tbody');
  tb.prepend(h('tr', {}, ...[tl.iter, `${tl.delayMs} ms`, tl.sent || 0, tl.acked || 0, tl.errs || 0, tl.inflight || 0, tl.keys || 0, `${tl.recoveryMs} ms`, tl.note || '']
    .map((v) => h('td', { text: String(v), title: String(v) }))));
  while (tb.rows.length > 200) tb.lastElementChild.remove();
  scheduleCharts();
}

function drawTiles(tl) {
  const s = T.sum || { sent: 0, acked: 0, errs: 0, inflight: 0 };
  const tiles = [
    ['iterations passed', tl ? `${tl.iter}/${tl.of}` : '–'], ['ops sent', s.sent], ['ops acked (must survive)', s.acked],
    ['in flight at kills', s.inflight], ['refused (ERR)', s.errs], ['last kill delay', tl ? `${tl.delayMs} ms` : '–'],
    ['keys after recovery', tl ? tl.keys : '–'],
  ];
  $('#t-tiles').replaceChildren(...tiles.map(([k, v]) => h('div', { class: 'tile' }, h('div', { class: 'k', text: k }), h('div', { class: 'v', text: String(v) }))));
}

async function drawCycle(tl) {
  const vals = tl ? [
    'READY', `sent ${tl.sent} · acked ${tl.acked}`, `after ${tl.delayMs} ms${tl.inflight ? ` · ${tl.inflight} in flight` : ''}${tl.note ? ' · ' + tl.note : ''}`,
    `recovery ${tl.recoveryMs} ms`, 'OK', `${tl.keys} keys`, 'PASS',
  ] : STATIONS.map(() => '–');
  const lis = STATIONS.map(([id, k], i) => h('li', { class: id }, h('div', { class: 'k', text: k }), h('div', { class: 'v', text: vals[i] })));
  $('#t-cycle').replaceChildren(...lis);
  if (!tl) return;
  const my = ++T.cycleToken;
  const d = reduceMotion ? 0 : Math.min(60, speed() / 2);
  for (const li of lis) {
    if (my !== T.cycleToken) return;
    li.classList.add('on');
    if (d) await sleep(d);
    if (li !== lis[lis.length - 1]) li.classList.remove('on');
  }
}

function scheduleCharts() {
  if (T.drawn) return;
  T.drawn = true;
  requestAnimationFrame(() => {
    T.drawn = false;
    bars('#t-acked', T.rows.map((r) => r.acked || 0), 'b1');
    bars('#t-delay', T.rows.map((r) => r.delayMs || 0), 'b2');
  });
}

function bars(sel, values, cls) {
  const svg = $(sel);
  const shown = values.slice(-300);
  const max = Math.max(1, ...shown);
  const w = 100 / Math.max(shown.length, 1);
  svg.replaceChildren(...shown.map((v, i) => svgEl('rect', {
    x: (i * w).toFixed(3), width: Math.max(w * 0.8, 0.2).toFixed(3), y: (40 - (40 * v) / max).toFixed(3), height: ((40 * v) / max).toFixed(3), class: cls,
  })));
}

// ---------- tabs, theme, wiring ----------

function selectTab(tab) {
  for (const t of $$('[role="tab"]')) {
    const on = t === tab;
    t.setAttribute('aria-selected', String(on));
    t.tabIndex = on ? 0 : -1;
    document.getElementById(t.getAttribute('aria-controls')).hidden = !on;
  }
  for (const st of ['db', 'torture', 'tests']) $('#stage-' + st).hidden = tab.dataset.stage !== st;
  try { sessionStorage.setItem('nl-tab', tab.id); } catch { /* fine */ }
}

function wireTabs() {
  const tabs = $$('[role="tab"]');
  for (const t of tabs) {
    t.addEventListener('click', () => selectTab(t));
    t.addEventListener('keydown', (e) => {
      const i = tabs.indexOf(t);
      const j = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: tabs.length - 1 }[e.key];
      if (j === undefined) return;
      e.preventDefault();
      const next = tabs[(j + tabs.length) % tabs.length];
      next.focus();
      selectTab(next);
    });
  }
  let saved = null;
  try { saved = sessionStorage.getItem('nl-tab'); } catch { /* fine */ }
  if (saved && document.getElementById(saved)) selectTab(document.getElementById(saved));
}

function wireTheme() {
  const root = document.documentElement;
  try {
    const t = localStorage.getItem('nl-theme');
    if (t === 'light' || t === 'dark') root.dataset.theme = t;
  } catch { /* fine */ }
  $('#theme').addEventListener('click', () => {
    const dark = root.dataset.theme ? root.dataset.theme === 'dark' : matchMedia('(prefers-color-scheme: dark)').matches;
    root.dataset.theme = dark ? 'light' : 'dark';
    try { localStorage.setItem('nl-theme', root.dataset.theme); } catch { /* fine */ }
  });
}

function wireForms() {
  $('#f-put').addEventListener('submit', (e) => {
    e.preventDefault();
    const f = e.target.elements;
    runAction('put', [f.key.value, f.value.value], num('#maxbytes'));
  });
  $('#f-key').addEventListener('submit', (e) => {
    e.preventDefault();
    const act = (e.submitter && e.submitter.value) || 'get';
    runAction(act, [e.target.elements.key.value], num('#maxbytes'));
  });
  $('#f-batch').addEventListener('submit', (e) => {
    e.preventDefault();
    const ops = e.target.elements.ops.value.split('\n').map((l) => l.replace(/\r$/, '')).filter((l) => l.trim() !== '');
    runAction('batch', ops, num('#maxbytes'));
  });
  $('#f-torture').addEventListener('submit', (e) => {
    e.preventDefault();
    const f = e.target.elements;
    runAction('torture', [f.mode.value, f.seed.value.trim(), f.iters.value], Math.max(0, Math.floor(Number(f.max.value) || 0)));
  });
  $('#f-serve').addEventListener('submit', (e) => { e.preventDefault(); serveSend(); });
  $('#serve-line').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      serveSend();
    } else if (e.key === 'ArrowUp' && !e.target.value && S.serve && S.serve.history.length) {
      e.preventDefault();
      e.target.value = S.serve.history[S.serve.history.length - 1];
    }
  });
  for (const c of $$('#serve-templates .chip')) {
    c.addEventListener('click', () => {
      $('#serve-line').value = c.dataset.tpl;
      $('#serve-line').focus();
    });
  }
}

// One handler for every plain action button (form buttons submit instead).
function wireActions() {
  document.addEventListener('click', (e) => {
    const b = e.target.closest('button[data-act]');
    if (!b || b.disabled || b.closest('form')) return;
    const act = b.dataset.act;
    if (act === 'serve') return startServe();
    if (act === 'crash-all') return runCrashAll();
    if (act === 'crash') return runCrash(b.dataset.arg);
    if (act === 'f4') return runCrash('f4');
    runAction(act, [], CLI_ACTS.has(act) ? num('#maxbytes') : 0);
  });
  $('#serve-kill').addEventListener('click', () => api('/api/serve/kill', {}).catch((e) => conError(e.message)));
  $('#serve-eof').addEventListener('click', () => api('/api/serve/eof', {}).catch((e) => conError(e.message)));
  $('#stop').addEventListener('click', () => {
    if (!S.running) return;
    const path = S.running.kind === 'serve' ? '/api/serve/eof' : '/api/cancel';
    api(path, {}).catch(() => S.running && S.running.abort.abort());
  });
  $('#replay').addEventListener('click', () => { if (P.current) { P.queue = []; playStep(P.current); } });
  $('#con-clear').addEventListener('click', () => { con.el.replaceChildren(); con.blocks = 0; });
}

function fillFromModel() {
  for (const p of S.model.paths) S.paths[p.id] = p;
  $('#t-mode').replaceChildren(...S.model.modes.map((m) => h('option', { value: m, text: m })));
  $('#tests-list').replaceChildren(...S.model.actions.filter((a) => a.panel === 'tests')
    .map((a) => h('button', { class: 'btn', type: 'button', 'data-act': a.name, title: a.desc, text: a.name })));
  buildCrashList();
}

async function init() {
  con.el = $('#console');
  wireTheme();
  wireTabs();
  wireForms();
  wireActions();
  wireWatch();
  drawTiles(null);
  drawCycle(null);
  serveControls();
  if (!TOKEN) {
    banner('This page needs its session token. Open the exact URL that nlui printed in its terminal.');
    return;
  }
  try {
    S.model = await api('/api/model');
  } catch (e) {
    banner('Cannot reach nlui: ' + e.message);
    return;
  }
  fillFromModel();
  await refreshState(true);
}

init();
