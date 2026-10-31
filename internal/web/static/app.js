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

// setView replaces a page's content, skipping false/null like el() does,
// so `cond && el(...)` never renders the text "false".
function setView(view, ...children) {
  view.replaceChildren(...children.flat().filter((c) => c !== null && c !== undefined && c !== false));
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

// parseCell returns the hours typed in a grid cell (0 when blank), or null
// if the text isn't valid.
function parseCell(text) {
  text = text.trim();
  if (text === '') return 0;
  if (!HOURS_RE.test(text) || Number(text) > 24) return null;
  return Number(text);
}

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
  setView(view, el('p', { class: 'error-text', text: err.message }));
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
  // The nav sits at the table's right edge, with "This week" leftmost, so
  // Prev/Next don't move as the title width changes or "This week" toggles.
  const toolbar = el('div', { class: 'toolbar' },
    el('h1', { text: title }),
    el('div', { class: 'toolbar-end' },
      start !== mondayOf(today) && el('a', { class: 'button', href: `#/week/${mondayOf(today)}`, text: 'This week' }),
      el('a', { class: 'button', href: `#/week/${addDays(start, -7)}`, text: '← Prev' }),
      el('a', { class: 'button', href: `#/week/${addDays(start, 7)}`, text: 'Next →' })));

  if (week.rows.length === 0) {
    setView(view, toolbar, el('p', {}, 'No projects yet. Add one on the ',
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
    const hours = parseCell(input.value);
    if (hours === null) {
      markError(input, 'Enter hours from 0 to 24, like 1.5 (up to two decimals).');
      return;
    }
    try {
      await api('PUT', '/api/entries', { projectId: week.rows[r].projectId, date: week.days[c], hours });
      grid[r][c] = toHundredths(hours);
      input.value = hours === 0 ? '' : fmtMin(grid[r][c]);
      delete input.dataset.dirty;
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
          'data-project': row.projectId, 'data-date': week.days[c],
          'aria-label': `${row.name}, ${week.days[c]}`,
          oninput: (e) => { e.target.dataset.dirty = '1'; },
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
  setView(view, el('div', { class: 'fit' }, toolbar, el('div', { class: 'table-wrap' }, table)));
}

// ---------- other pages ----------

function shiftMonth(month, n) {
  const [y, m] = month.split('-').map(Number);
  const d = new Date(y, m - 1 + n, 1);
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}`;
}

function copyButton(label, value) {
  const status = el('span', { class: 'copied', 'aria-live': 'polite' });
  let timer;
  const btn = el('button', {
    class: 'small', type: 'button', 'aria-label': label, title: 'Copy to clipboard', text: 'Copy',
    onclick: async () => {
      try {
        await navigator.clipboard.writeText(value);
        status.textContent = 'Copied';
        status.className = 'copied';
      } catch {
        status.textContent = 'Copy failed';
        status.className = 'copied error-text';
      }
      clearTimeout(timer);
      timer = setTimeout(() => { status.textContent = ''; }, 1500);
    },
  });
  return [btn, status];
}

async function renderMonth(view, month) {
  const data = await api('GET', `/api/month?month=${month}`);
  const [y, m] = month.split('-').map(Number);
  const title = new Date(y, m - 1, 1).toLocaleDateString(undefined, { month: 'long', year: 'numeric' });
  const current = todayLocal().slice(0, 7);
  // Same layout as the week page: nav at the table's right edge, "This month" leftmost.
  const toolbar = el('div', { class: 'toolbar' },
    el('h1', { text: title }),
    el('div', { class: 'toolbar-end' },
      month !== current && el('a', { class: 'button', href: `#/month/${current}`, text: 'This month' }),
      el('a', { class: 'button', href: `#/month/${shiftMonth(month, -1)}`, text: '← Prev' }),
      el('a', { class: 'button', href: `#/month/${shiftMonth(month, 1)}`, text: 'Next →' })));

  if (data.rows.length === 0) {
    setView(view, toolbar, el('p', { class: 'muted', text: `No hours logged in ${title}.` }));
    return;
  }
  const rows = data.rows.map((row) => {
    const shown = fmtTotal(toHundredths(row.hours));
    return el('tr', {},
      el('td', {}, row.name, row.archived && el('span', { class: 'tag', text: ' (archived)' })),
      el('td', { class: 'copy-cell' }, shown, copyButton(`Copy hours for ${row.name}`, shown)));
  });
  const total = fmtTotal(toHundredths(data.total));
  setView(view, el('div', { class: 'fit summary' }, toolbar, el('div', { class: 'table-wrap' }, el('table', {},
    el('thead', {}, el('tr', {}, el('th', { text: 'Project' }), el('th', { text: 'Hours' }))),
    el('tbody', {}, rows),
    el('tfoot', {}, el('tr', {}, el('td', { text: 'Total' }),
      el('td', { class: 'copy-cell' }, total, copyButton('Copy total hours', total))))))));
}

async function renderProjects(view) {
  const projects = await api('GET', '/api/projects');
  const rerender = () => renderProjects(view).catch((err) => showError(view, err));

  const addInput = el('input', { type: 'text', placeholder: 'New project name', maxlength: '100', 'aria-label': 'New project name' });
  const addError = el('p', { class: 'error-text' });
  const addForm = el('form', {
    class: 'inline',
    onsubmit: async (e) => {
      e.preventDefault();
      try {
        await api('POST', '/api/projects', { name: addInput.value });
        await rerender();
        view.querySelector('form.inline input')?.focus();
      } catch (err) {
        addError.textContent = err.message;
      }
    },
  }, addInput, el('button', { class: 'primary', type: 'submit', text: 'Add' }));

  function activeItem(p) {
    const li = el('li');
    const err = el('span', { class: 'error-text' });
    const show = () => li.replaceChildren(
      el('span', { class: 'name', text: p.name }), err,
      el('button', { class: 'small', type: 'button', text: 'Rename', onclick: edit }),
      el('button', { class: 'small', type: 'button', text: 'Archive', onclick: () => setArchived(true) }));
    async function setArchived(archived) {
      try {
        await api('PATCH', `/api/projects/${p.id}`, { archived });
        await rerender();
      } catch (e) { err.textContent = e.message; }
    }
    function edit() {
      const input = el('input', { type: 'text', value: p.name, maxlength: '100', 'aria-label': `New name for ${p.name}` });
      const save = async () => {
        try {
          await api('PATCH', `/api/projects/${p.id}`, { name: input.value });
          await rerender();
        } catch (e) { err.textContent = e.message; }
      };
      input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') { e.preventDefault(); save(); }
        if (e.key === 'Escape') { err.textContent = ''; show(); }
      });
      li.replaceChildren(input, err,
        el('button', { class: 'small primary', type: 'button', text: 'Save', onclick: save }),
        el('button', { class: 'small', type: 'button', text: 'Cancel', onclick: () => { err.textContent = ''; show(); } }));
      input.focus();
      input.select();
    }
    show();
    return li;
  }

  function archivedItem(p) {
    const err = el('span', { class: 'error-text' });
    return el('li', {},
      el('span', { class: 'name', text: p.name }), err,
      el('button', {
        class: 'small', type: 'button', text: 'Restore',
        onclick: async () => {
          try {
            await api('PATCH', `/api/projects/${p.id}`, { archived: false });
            await rerender();
          } catch (e) { err.textContent = e.message; }
        },
      }));
  }

  const active = projects.filter((p) => !p.archived);
  const archived = projects.filter((p) => p.archived);
  setView(view, 
    el('h1', { text: 'Projects' }),
    addForm, addError,
    el('h2', { text: 'Active' }),
    active.length ? el('ul', { class: 'list' }, active.map(activeItem)) : el('p', { class: 'muted', text: 'No active projects.' }),
    archived.length > 0 && el('h2', { text: 'Archived' }),
    archived.length > 0 && el('p', { class: 'muted', text: 'Hidden from the week grid. Their hours still count in month totals.' }),
    archived.length > 0 && el('ul', { class: 'list archived' }, archived.map(archivedItem)));
}

const fmtStamp = (iso) => new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });

// confirmPanel asks before replacing all data; onYes returns a promise.
function confirmPanel(message, onYes) {
  const result = el('p');
  const panel = el('div', { class: 'panel warn' },
    el('p', { text: message }),
    el('div', { class: 'actions' },
      el('button', {
        class: 'primary', type: 'button', text: 'Continue',
        onclick: async (e) => {
          e.target.disabled = true;
          try {
            await onYes();
            panel.replaceChildren(el('p', { class: 'ok-text', text: 'Restored. Your data has been replaced.' }));
          } catch (err) {
            result.className = 'error-text';
            result.textContent = `Restore failed: ${err.message}`;
            e.target.disabled = false;
          }
        },
      }),
      el('button', { type: 'button', text: 'Cancel', onclick: () => panel.remove() })),
    result);
  return panel;
}

async function renderBackup(view) {
  const [status, snapshots] = await Promise.all([api('GET', '/api/backup/status'), api('GET', '/api/snapshots')]);
  const rerender = () => renderBackup(view).catch((err) => showError(view, err));
  const replaceWarning = 'This will replace all current data. Your current data is saved as a "Before restore" snapshot first. Continue?';

  const download = el('a', {
    class: 'button primary', href: '/api/backup/download', download: true, text: 'Download backup',
    onclick: () => setTimeout(() => { refreshBackupDot(); rerender(); }, 1000),
  });

  const confirmSlot = el('div');
  const fileError = el('p', { class: 'error-text' });
  const fileInput = el('input', {
    type: 'file', accept: '.json,application/json', 'aria-label': 'Backup file to restore',
    onchange: async () => {
      fileError.textContent = '';
      confirmSlot.replaceChildren();
      const file = fileInput.files[0];
      if (!file) return;
      try {
        const text = await file.text();
        const summary = await api('POST', '/api/backup/check', text);
        confirmSlot.replaceChildren(confirmPanel(
          `This file has ${summary.projects} projects and ${summary.entries} hour entries. ${replaceWarning}`,
          async () => {
            await api('POST', '/api/backup/restore', text);
            fileInput.value = '';
            setTimeout(rerender, 1200);
          }));
      } catch (err) {
        fileError.textContent = `Can't restore this file: ${err.message}`;
      }
    },
  });

  const snapSlot = el('div');
  const snapList = snapshots.length === 0
    ? el('p', { class: 'muted', text: 'No snapshots yet. One is taken each day once you have data.' })
    : el('ul', { class: 'list' }, snapshots.map((s) => el('li', {},
      el('span', { class: 'name' }, fmtStamp(s.createdAt), el('span', { class: 'tag', text: s.kind === 'daily' ? '  Daily' : '  Before restore' })),
      el('button', {
        class: 'small', type: 'button', text: 'Restore',
        onclick: () => snapSlot.replaceChildren(confirmPanel(replaceWarning, async () => {
          await api('POST', `/api/snapshots/${encodeURIComponent(s.name)}/restore`, {});
          setTimeout(rerender, 1200);
        })),
      }))));

  setView(view, 
    el('h1', { text: 'Backup' }),
    status.reminder && el('div', { class: 'panel warn' },
      el('p', { text: "It's been a while since your last backup. Download one and keep it somewhere safe (cloud drive, USB stick)." })),
    el('div', { class: 'panel' },
      el('p', {}, 'Last backup downloaded: ', el('strong', { text: status.lastDownloadAt ? fmtStamp(status.lastDownloadAt) : 'Never' })),
      el('p', { class: 'muted', text: 'The file holds all your projects and hours. Keep it off this computer so it survives a dead laptop.' }),
      download),
    el('h2', { text: 'Restore from file' }),
    el('div', { class: 'panel' }, fileInput, fileError),
    confirmSlot,
    el('h2', { text: 'Automatic snapshots' }),
    el('p', { class: 'muted', text: 'A copy of your data is kept each day (last 14 days), plus one before every restore.' }),
    snapSlot,
    snapList);
}

// ---------- unsaved cells on close ----------

// Closing or reloading the tab doesn't fire `change`, so a number typed into
// the focused cell would be lost. Send dirty cells with keepalive, which
// outlives the page; ask before leaving if one can't be saved.
const dirtyCells = () => [...document.querySelectorAll('table.grid input[data-dirty]')];

window.addEventListener('pagehide', () => {
  for (const input of dirtyCells()) {
    const hours = parseCell(input.value);
    if (hours === null) continue;
    fetch('/api/entries', {
      method: 'PUT',
      keepalive: true,
      headers: { 'X-PTT-Request': '1', 'Content-Type': 'application/json' },
      body: JSON.stringify({ projectId: input.dataset.project, date: input.dataset.date, hours }),
    });
  }
});

window.addEventListener('beforeunload', (e) => {
  if (dirtyCells().some((input) => parseCell(input.value) === null)) e.preventDefault();
});

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
