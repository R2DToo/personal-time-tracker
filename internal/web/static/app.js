'use strict';

// ---------- helpers ----------

// el builds a DOM element. Text is always set via textContent, never
// innerHTML, so project names can't inject markup.
function el(tag, attrs, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k.startsWith('on')) node.addEventListener(k.slice(2), v);
    else if (k === 'class') node.className = v;
    else if (k === 'text') node.textContent = v;
    else if (v === true) node.setAttribute(k, '');
    else node.setAttribute(k, v);
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    node.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return node;
}

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (method !== 'GET') {
    opts.headers['X-PTT-Request'] = '1';
    opts.headers['Content-Type'] = 'application/json';
    opts.body = typeof body === 'string' ? body : JSON.stringify(body ?? {});
  }
  const res = await fetch(path, opts);
  if (res.status === 204) return null;
  let data = null;
  try { data = await res.json(); } catch { /* non-JSON body */ }
  if (!res.ok) throw new Error((data && data.error) || `Request failed (${res.status})`);
  return data;
}

// Hours travel as decimals and are summed as integer hundredths.
const toHundredths = (n) => Math.round(n * 100);

function fmtMin(h) {
  const whole = Math.trunc(h / 100), frac = h % 100;
  if (frac === 0) return String(whole);
  if (frac % 10 === 0) return `${whole}.${frac / 10}`;
  return `${whole}.${String(frac).padStart(2, '0')}`;
}

function fmtTotal(h) {
  const s = fmtMin(h);
  return s.includes('.') ? s : `${s}.0`;
}

const HOURS_RE = /^(\d+(\.\d{1,2})?|\.\d{1,2})$/;

// Local calendar dates as YYYY-MM-DD; never toISOString(), which is UTC.
const pad = (n) => String(n).padStart(2, '0');
const ymd = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
function parseYmd(s) {
  const [y, m, d] = s.split('-').map(Number);
  return new Date(y, m - 1, d);
}
const todayLocal = () => ymd(new Date());
function addDays(s, n) {
  const d = parseYmd(s);
  d.setDate(d.getDate() + n);
  return ymd(d);
}
function mondayOf(s) {
  const d = parseYmd(s);
  return addDays(s, -((d.getDay() + 6) % 7));
}

function showError(view, err) {
  view.replaceChildren(el('p', { class: 'error-text', text: err.message }));
}

// ---------- backup reminder dot ----------

async function refreshBackupDot() {
  try {
    const st = await api('GET', '/api/backup/status');
    document.querySelector('.sidebar .dot').hidden = !st.reminder;
  } catch { /* leave the dot as it was */ }
}

// ---------- week page ----------

async function renderWeek(view, start) {
  const week = await api('GET', `/api/week?start=${start}`);
  const today = todayLocal();
  const grid = week.rows.map((r) => r.hours.map(toHundredths));

  const title = 'Week of ' + parseYmd(start).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
  const toolbar = el('div', { class: 'toolbar' },
    el('h1', { text: title }),
    el('a', { class: 'button', href: `#/week/${addDays(start, -7)}`, text: '← Prev' }),
    el('a', { class: 'button', href: `#/week/${addDays(start, 7)}`, text: 'Next →' }),
    start !== mondayOf(today) && el('a', { class: 'button', href: `#/week/${mondayOf(today)}`, text: 'This week' }));

  if (week.rows.length === 0) {
    view.replaceChildren(toolbar, el('p', {}, 'No projects yet. Add one on the ',
      el('a', { href: '#/projects', text: 'Projects page' }), '.'));
    return;
  }

  const dayTh = week.days.map((d) => {
    const dt = parseYmd(d);
    return el('th', { class: d === today ? 'today' : null },
      dt.toLocaleDateString(undefined, { weekday: 'short' }) + ' ' + dt.getDate());
  });
  const rowTotals = [], dayTotals = week.days.map(() => el('td')), weekTotal = el('td');

  function recompute() {
    let all = 0;
    const days = week.days.map(() => 0);
    grid.forEach((hours, r) => {
      const sum = hours.reduce((a, b) => a + b, 0);
      rowTotals[r].textContent = fmtMin(sum);
      hours.forEach((h, c) => { days[c] += h; });
      all += sum;
    });
    days.forEach((h, c) => { dayTotals[c].textContent = fmtMin(h); });
    weekTotal.textContent = fmtMin(all);
  }

  async function save(input, r, c) {
    const text = input.value.trim();
    let hours = 0;
    if (text !== '') {
      if (!HOURS_RE.test(text) || Number(text) > 24) {
        markError(input, 'Enter hours from 0 to 24, like 1.5 (up to two decimals).');
        return;
      }
      hours = Number(text);
    }
    try {
      await api('PUT', '/api/entries', { projectId: week.rows[r].projectId, date: week.days[c], hours });
      grid[r][c] = toHundredths(hours);
      input.value = hours === 0 ? '' : fmtMin(grid[r][c]);
      input.classList.remove('error');
      input.removeAttribute('title');
      recompute();
    } catch (err) {
      markError(input, `Not saved: ${err.message}`);
    }
  }

  function markError(input, msg) {
    input.classList.add('error');
    input.title = msg;
  }

  function focusCell(r, c) {
    const target = view.querySelector(`input[data-r="${r}"][data-c="${c}"]`);
    if (target) { target.focus(); target.select(); }
  }

  function onKey(e, r, c) {
    const input = e.target;
    const atStart = input.selectionStart === 0 && input.selectionEnd === 0;
    const atEnd = input.selectionStart === input.value.length && input.selectionEnd === input.value.length;
    if (e.key === 'Enter' || e.key === 'ArrowDown') { e.preventDefault(); input.blur(); focusCell(r + 1, c); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); input.blur(); focusCell(r - 1, c); }
    else if (e.key === 'ArrowLeft' && atStart) { e.preventDefault(); focusCell(r, c - 1); }
    else if (e.key === 'ArrowRight' && atEnd) { e.preventDefault(); focusCell(r, c + 1); }
  }

  const body = week.rows.map((row, r) => {
    rowTotals[r] = el('td');
    return el('tr', {},
      el('td', {}, row.name, row.archived && el('span', { class: 'tag', text: ' (archived)' })),
      grid[r].map((h, c) => el('td', { class: 'cell' + (week.days[c] === today ? ' today' : '') },
        el('input', {
          type: 'text', inputmode: 'decimal', autocomplete: 'off',
          'data-r': r, 'data-c': c, value: h ? fmtMin(h) : '',
          'aria-label': `${row.name}, ${week.days[c]}`,
          onchange: (e) => save(e.target, r, c),
          onkeydown: (e) => onKey(e, r, c),
        }))),
      rowTotals[r]);
  });

  const table = el('table', { class: 'grid' },
    el('thead', {}, el('tr', {}, el('th', { text: 'Project' }), dayTh, el('th', { text: 'Total' }))),
    el('tbody', {}, body),
    el('tfoot', {}, el('tr', {}, el('td', { text: 'Day total' }), dayTotals, weekTotal)));
  recompute();
  view.replaceChildren(toolbar, el('div', { class: 'table-wrap' }, table));
}

// ---------- other pages ----------

async function renderMonth(view) {
  view.replaceChildren(el('h1', { text: 'Month' }));
}

async function renderProjects(view) {
  view.replaceChildren(el('h1', { text: 'Projects' }));
}

async function renderBackup(view) {
  view.replaceChildren(el('h1', { text: 'Backup' }));
}

// ---------- router ----------

function route() {
  const [page, arg] = location.hash.replace(/^#\/?/, '').split('/');
  const view = document.getElementById('view');
  let render;
  if (page === 'week' && arg && /^\d{4}-\d{2}-\d{2}$/.test(arg) && ymd(parseYmd(arg)) === arg && mondayOf(arg) === arg) {
    render = () => renderWeek(view, arg);
  } else if (page === 'month' && arg && /^\d{4}-(0[1-9]|1[0-2])$/.test(arg)) {
    render = () => renderMonth(view, arg);
  } else if (page === 'month') {
    location.replace(`#/month/${todayLocal().slice(0, 7)}`);
    return;
  } else if (page === 'projects') {
    render = () => renderProjects(view);
  } else if (page === 'backup') {
    render = () => renderBackup(view);
  } else {
    location.replace(`#/week/${mondayOf(todayLocal())}`);
    return;
  }
  for (const a of document.querySelectorAll('[data-nav]')) {
    if (a.dataset.nav === page) a.setAttribute('aria-current', 'page');
    else a.removeAttribute('aria-current');
  }
  render().catch((err) => showError(view, err));
  refreshBackupDot();
}

window.addEventListener('hashchange', route);
route();
