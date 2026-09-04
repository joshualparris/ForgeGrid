// AgentBridge local GUI.
//
// Every AgentBridge API call requires "Authorization: Bearer <name>:<secret>"
// (see internal/agentbridge/server.go authenticate()). There is no session
// cookie and no "/api/me" endpoint - identity is whatever credentials this
// page attaches to each request. Credentials are kept in localStorage (this
// page is served locally, from the same machine running the agent) and
// never sent anywhere except this node's own AgentBridge API.

const STORAGE_KEY = 'agentbridge.credentials';
const PINNED_KEY = 'agentbridge.pinned';

const el = (id) => document.getElementById(id);

const loginScreen = el('login-screen');
const loginForm = el('login-form');
const loginError = el('login-error');
const appContainer = el('app-container');

const myNameEl = el('my-name');
const myStatusEl = el('my-status');
const inboxList = el('inbox-list');
const refreshBtn = el('refresh-btn');
const logoutBtn = el('logout-btn');
const includeOutgoing = el('include-outgoing');
const includeResolved = el('include-resolved');

const sendForm = el('send-form');
const submitBtn = el('submit-btn');
const recipientInput = el('recipient');
const msgTypeSelect = el('msg-type');
const taskIdInput = el('task-id');
const taskIdHint = el('task-id-hint');
const bodyInput = el('body');
const knownAgentsDatalist = el('known-agents');

const pinnedSection = el('pinned-section');
const pinnedList = el('pinned-list');

const detailPanel = el('message-detail');
const closeDetailBtn = el('close-detail');
const detailSender = el('detail-sender');
const detailRecipient = el('detail-recipient');
const detailTask = el('detail-task');
const detailType = el('detail-type');
const detailStatus = el('detail-status');
const detailTime = el('detail-time');
const detailBody = el('detail-body');
const detailActions = el('detail-actions');
const pinBtn = el('pin-btn');

const toast = el('toast');
const toastMsg = el('toast-msg');

const TYPES_REQUIRING_TASK_ID = new Set(['instruction', 'progress', 'result']);

let credentials = null; // { name, secret }
let messages = [];
let pinnedContacts = JSON.parse(localStorage.getItem(PINNED_KEY) || '[]');
let currentDetailMessage = null;

function showToast(msg, isError) {
  toastMsg.textContent = msg;
  toast.classList.remove('hidden');
  toast.classList.toggle('toast-error', !!isError);
  clearTimeout(showToast._t);
  showToast._t = setTimeout(() => toast.classList.add('hidden'), 3500);
}

function authHeader() {
  return 'Bearer ' + credentials.name + ':' + credentials.secret;
}

async function api(path, options) {
  const res = await fetch(path, Object.assign({}, options, {
    headers: Object.assign({ 'Authorization': authHeader() }, (options && options.headers) || {}),
  }));
  if (!res.ok) {
    let detail = res.statusText;
    try { detail = (await res.text()) || detail; } catch (_) { /* ignore */ }
    const err = new Error(detail);
    err.status = res.status;
    throw err;
  }
  const contentType = res.headers.get('content-type') || '';
  if (contentType.includes('application/json')) return res.json();
  return null;
}

function formatDate(dateString) {
  const d = new Date(dateString);
  if (isNaN(d.getTime())) return dateString;
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) + ' - ' + d.toLocaleDateString();
}

function randomIdempotencyKey() {
  return 'ui-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 10);
}

// ---- Auth ----

function loadStoredCredentials() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch (_) {
    return null;
  }
}

function saveCredentials(creds) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify(creds));
}

function clearCredentials() {
  localStorage.removeItem(STORAGE_KEY);
}

async function tryConnect(creds) {
  credentials = creds;
  await api('/api/v1/agent-status');
  myNameEl.textContent = creds.name;
  myStatusEl.textContent = 'Connected';
  loginScreen.classList.add('hidden');
  appContainer.classList.remove('hidden');
  saveCredentials(creds);
  await Promise.all([loadInbox(), loadAgents()]);
}

loginForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  loginError.classList.add('hidden');
  const creds = { name: el('login-name').value.trim(), secret: el('login-secret').value };
  if (!creds.name || !creds.secret) return;
  try {
    await tryConnect(creds);
  } catch (err) {
    credentials = null;
    loginError.textContent = err.status === 401
      ? 'Could not authenticate with that name/secret.'
      : 'Could not reach AgentBridge: ' + err.message;
    loginError.classList.remove('hidden');
  }
});

logoutBtn.addEventListener('click', () => {
  clearCredentials();
  credentials = null;
  appContainer.classList.add('hidden');
  loginScreen.classList.remove('hidden');
  el('login-secret').value = '';
});

// ---- Inbox ----

async function loadInbox() {
  refreshBtn.classList.add('spinning');
  inboxList.innerHTML = '<div class="loading">Loading messages...</div>';
  try {
    const params = new URLSearchParams();
    params.set('include_outgoing', includeOutgoing.checked ? 'true' : 'false');
    if (includeResolved.checked) {
      params.set('status', 'pending,acknowledged,completed,failed,expired');
    }
    messages = await api('/api/v1/agent-messages/inbox?' + params.toString());
    renderInbox();
    updateKnownAgents();
    renderPinnedContacts();
  } catch (err) {
    inboxList.innerHTML = `<div class="loading error">Error: ${escapeHtml(err.message)}</div>`;
  } finally {
    refreshBtn.classList.remove('spinning');
  }
}

async function loadAgents() {
  try {
    const agents = await api('/api/v1/agent-messages/agents');
    knownAgentsDatalist.innerHTML = '';
    (agents || []).forEach((agent) => {
      const opt = document.createElement('option');
      opt.value = agent;
      knownAgentsDatalist.appendChild(opt);
    });
  } catch (_) {
    // Non-fatal - recipient autocomplete just won't be populated.
  }
}

function updateKnownAgents() {
  messages.forEach((msg) => {
    if (msg.sender && !hasOption(msg.sender)) addOption(msg.sender);
    if (msg.recipient && msg.recipient !== '#all-agents' && !hasOption(msg.recipient)) addOption(msg.recipient);
  });
}

function hasOption(value) {
  return Array.from(knownAgentsDatalist.options).some((o) => o.value === value);
}

function addOption(value) {
  const opt = document.createElement('option');
  opt.value = value;
  knownAgentsDatalist.appendChild(opt);
}

function escapeHtml(s) {
  const div = document.createElement('div');
  div.textContent = s == null ? '' : String(s);
  return div.innerHTML;
}

function renderInbox() {
  inboxList.innerHTML = '';
  if (messages.length === 0) {
    inboxList.innerHTML = '<div class="loading">No messages.</div>';
    return;
  }
  messages
    .slice()
    .sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
    .forEach((msg) => {
      const row = document.createElement('div');
      row.className = 'message-row status-' + msg.status;
      row.innerHTML = `
        <div class="message-row-top">
          <span class="message-sender">${escapeHtml(msg.sender)} &rarr; ${escapeHtml(msg.recipient)}</span>
          <span class="badge badge-${escapeHtml(msg.type)}">${escapeHtml(msg.type)}</span>
        </div>
        <div class="message-preview">${escapeHtml((msg.body || '').slice(0, 120))}</div>
        <div class="message-row-bottom">
          <span class="message-time">${formatDate(msg.created_at)}</span>
          <span class="badge badge-status-${escapeHtml(msg.status)}">${escapeHtml(msg.status)}</span>
        </div>
      `;
      row.addEventListener('click', () => openDetail(msg));
      inboxList.appendChild(row);
    });
}

// ---- Detail panel ----

function openDetail(msg) {
  currentDetailMessage = msg;
  detailSender.textContent = msg.sender;
  detailRecipient.textContent = msg.recipient;
  detailTask.textContent = msg.task_id || '(none)';
  detailType.textContent = msg.type;
  detailStatus.textContent = msg.status;
  detailTime.textContent = formatDate(msg.created_at);
  detailBody.textContent = msg.body;
  updatePinBtnState();
  renderDetailActions(msg);
  detailPanel.classList.remove('hidden');
}

closeDetailBtn.addEventListener('click', () => detailPanel.classList.add('hidden'));

function renderDetailActions(msg) {
  detailActions.innerHTML = '';
  const iAmRecipient = credentials && (msg.recipient === credentials.name || msg.recipient === '#all-agents');
  if (!iAmRecipient) return;

  const addActionBtn = (label, action, needsResult) => {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'btn-secondary';
    btn.textContent = label;
    btn.addEventListener('click', async () => {
      let result;
      if (needsResult) {
        const note = window.prompt('Optional result note (plain text, sent as {"note": ...}):', '');
        if (note === null) return; // cancelled
        result = { note };
      }
      try {
        await api(`/api/v1/agent-messages/${encodeURIComponent(msg.id)}/${action}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: needsResult ? JSON.stringify({ result }) : undefined,
        });
        showToast(`Message ${action}d`);
        detailPanel.classList.add('hidden');
        loadInbox();
      } catch (err) {
        showToast(`Failed to ${action}: ${err.message}`, true);
      }
    });
    detailActions.appendChild(btn);
  };

  if (msg.status === 'pending') addActionBtn('Acknowledge', 'ack', false);
  if (msg.status === 'pending' || msg.status === 'acknowledged') {
    addActionBtn('Complete', 'complete', true);
    addActionBtn('Fail', 'fail', true);
  }
}

// ---- Pinning ----

function togglePinContact(agent) {
  const idx = pinnedContacts.indexOf(agent);
  if (idx >= 0) {
    pinnedContacts.splice(idx, 1);
    showToast(agent + ' unpinned');
  } else {
    pinnedContacts.push(agent);
    showToast(agent + ' pinned');
  }
  localStorage.setItem(PINNED_KEY, JSON.stringify(pinnedContacts));
  renderPinnedContacts();
  updatePinBtnState();
}

function updatePinBtnState() {
  const pinned = currentDetailMessage && pinnedContacts.includes(currentDetailMessage.sender);
  pinBtn.classList.toggle('active', !!pinned);
  pinBtn.title = pinned ? 'Unpin Sender' : 'Pin Sender';
}

function renderPinnedContacts() {
  if (pinnedContacts.length === 0) {
    pinnedSection.classList.add('hidden');
    return;
  }
  pinnedSection.classList.remove('hidden');
  pinnedList.innerHTML = '';
  pinnedContacts.forEach((contact) => {
    const item = document.createElement('div');
    item.className = 'pinned-contact';
    item.innerHTML = `<div class="pinned-avatar">${escapeHtml(contact.charAt(0).toUpperCase())}</div><div class="pinned-name">${escapeHtml(contact)}</div>`;
    item.addEventListener('click', () => {
      recipientInput.value = contact;
      recipientInput.focus();
    });
    pinnedList.appendChild(item);
  });
}

pinBtn.addEventListener('click', () => {
  if (currentDetailMessage) togglePinContact(currentDetailMessage.sender);
});

// ---- Compose ----

msgTypeSelect.addEventListener('change', () => {
  const required = TYPES_REQUIRING_TASK_ID.has(msgTypeSelect.value);
  taskIdInput.required = required;
  taskIdHint.textContent = required ? '(required for this type)' : '(optional for this type)';
});

sendForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  submitBtn.disabled = true;
  try {
    await api('/api/v1/agent-messages', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        recipient: recipientInput.value.trim(),
        task_id: taskIdInput.value.trim(),
        type: msgTypeSelect.value,
        body: bodyInput.value,
        idempotency_key: randomIdempotencyKey(),
      }),
    });
    showToast('Message sent');
    sendForm.reset();
    msgTypeSelect.value = 'chat';
    loadInbox();
  } catch (err) {
    showToast('Send failed: ' + err.message, true);
  } finally {
    submitBtn.disabled = false;
  }
});

refreshBtn.addEventListener('click', loadInbox);
includeOutgoing.addEventListener('change', loadInbox);
includeResolved.addEventListener('change', loadInbox);

// ---- Boot ----

(async function init() {
  const stored = loadStoredCredentials();
  if (!stored) return;
  try {
    await tryConnect(stored);
  } catch (_) {
    clearCredentials();
  }
})();
