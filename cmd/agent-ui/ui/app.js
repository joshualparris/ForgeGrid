const inboxList = document.getElementById('inbox-list');
const refreshBtn = document.getElementById('refresh-btn');
const sendForm = document.getElementById('send-form');
const submitBtn = document.getElementById('submit-btn');
const toast = document.getElementById('toast');
const toastMsg = document.getElementById('toast-msg');
const recipientInput = document.getElementById('recipient');
const knownAgentsDatalist = document.getElementById('known-agents');
const pinnedSection = document.getElementById('pinned-section');
const pinnedList = document.getElementById('pinned-list');
const pinBtn = document.getElementById('pin-btn');

// Detail Panel Elements
const detailPanel = document.getElementById('message-detail');
const closeDetailBtn = document.getElementById('close-detail');
const detailSubject = document.getElementById('detail-subject');
const detailSender = document.getElementById('detail-sender');
const detailRecipient = document.getElementById('detail-recipient');
const detailTask = document.getElementById('detail-task');
const detailType = document.getElementById('detail-type');
const detailTime = document.getElementById('detail-time');
const detailBody = document.getElementById('detail-body');

let messages = [];
let pinnedContacts = JSON.parse(localStorage.getItem('pinnedContacts') || '[]');
let sentMessages = JSON.parse(localStorage.getItem('sentMessages') || '[]');
let currentDetailSender = '';

function showToast(msg) {
    toastMsg.textContent = msg;
    toast.classList.remove('hidden');
    setTimeout(() => {
        toast.classList.add('hidden');
    }, 3000);
}

function formatDate(dateString) {
    const d = new Date(dateString);
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) + ' - ' + d.toLocaleDateString();
}

async function loadMe() {
    try {
        const res = await fetch('/api/me');
        if (res.ok) {
            const data = await res.json();
            document.getElementById('my-name').textContent = data.name;
            document.getElementById('my-url').textContent = "Connected to: " + data.url;
        }
    } catch (err) {
        console.error("Failed to load agent info", err);
    }
}

async function loadInbox() {
    refreshBtn.classList.add('spinning');
    inboxList.innerHTML = '<div class="loading">Loading messages...</div>';
    
    try {
        const res = await fetch('/api/inbox');
        if (!res.ok) throw new Error("Failed to fetch inbox");
        
        const fetchedMessages = await res.json();
        
        // Merge with locally stored sent messages
        messages = [...fetchedMessages, ...sentMessages];
        messages.sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
        
        renderInbox();
        updateKnownAgents();
        renderPinnedContacts();
    } catch (err) {
        inboxList.innerHTML = `<div class="loading" style="color:var(--danger)">Error: ${err.message}</div>`;
    } finally {
        refreshBtn.classList.remove('spinning');
    }
}

function updateKnownAgents() {
    const agents = new Set();
    messages.forEach(msg => {
        if (msg.sender) agents.add(msg.sender);
        if (msg.recipient) agents.add(msg.recipient);
    });
    
    knownAgentsDatalist.innerHTML = '';
    agents.forEach(agent => {
        const option = document.createElement('option');
        option.value = agent;
        knownAgentsDatalist.appendChild(option);
    });
}

function togglePinContact(agent) {
    const idx = pinnedContacts.indexOf(agent);
    if (idx >= 0) {
        pinnedContacts.splice(idx, 1);
        showToast(agent + " unpinned");
    } else {
        pinnedContacts.push(agent);
        showToast(agent + " pinned");
    }
    localStorage.setItem('pinnedContacts', JSON.stringify(pinnedContacts));
    renderPinnedContacts();
    updatePinBtnState();
}

function updatePinBtnState() {
    if (pinnedContacts.includes(currentDetailSender)) {
        pinBtn.classList.add('active');
        pinBtn.title = "Unpin Sender";
    } else {
        pinBtn.classList.remove('active');
        pinBtn.title = "Pin Sender";
    }
}

function renderPinnedContacts() {
    if (pinnedContacts.length === 0) {
        pinnedSection.classList.add('hidden');
        return;
    }
    
    pinnedSection.classList.remove('hidden');
    pinnedList.innerHTML = '';
    
    pinnedContacts.forEach(contact => {
        const el = document.createElement('div');
        el.className = 'pinned-contact';
        const initial = contact.charAt(0).toUpperCase();
        el.innerHTML = `
            <div class="pinned-avatar">${initial}</div>
            <div class="pinned-name">${contact}</div>
        `;
        el.addEventListener('click', () => {
            recipientInput.value = contact;
            recipientInput.focus();
        });
        pinnedList.appendChild(el);
    });
}

pinBtn.addEventListener('click', () => {
    if (currentDetailSender) {
        togglePinContact(currentDetailSender);
    }
});

function renderInbox() {
    inboxList.innerHTML = '';
    
    if (!messages || messages.length === 0) {
        inboxList.innerHTML = '<div class="loading">No messages yet.</div>';
        return;
    }

    messages.forEach(msg => {
        const el = document.createElement('div');
        el.className = 'message-item';
        
        const isUnread = msg.status === 'pending';
        const isSent = msg.sender === document.getElementById('my-name').textContent;
        
        if (isUnread && !isSent) {
            el.classList.add('unread');
        }

        const preview = msg.body.substring(0, 40) + (msg.body.length > 40 ? '...' : '');
        const senderLabel = isSent ? `To: ${msg.recipient}` : msg.sender;

        el.innerHTML = `
            <div class="msg-header">
                <span class="msg-sender">${senderLabel} ${isSent ? '<small style="color:var(--text-secondary)">(Sent)</small>' : ''}</span>
                <span class="msg-time">${formatDate(msg.created_at).split(' - ')[0]}</span>
            </div>
            <div class="msg-preview">${preview}</div>
        `;
        
        el.addEventListener('click', () => openMessage(msg));
        inboxList.appendChild(el);
    });
}

function openMessage(msg) {
    currentDetailSender = msg.sender;
    updatePinBtnState();
    
    detailSender.textContent = msg.sender;
    detailRecipient.textContent = msg.recipient;
    detailTask.textContent = msg.task_id || 'N/A';
    detailType.textContent = msg.type;
    detailTime.textContent = formatDate(msg.created_at);
    detailBody.textContent = msg.body;
    
    detailSubject.textContent = `Message from ${msg.sender}`;
    detailPanel.classList.remove('hidden');
}

closeDetailBtn.addEventListener('click', () => {
    detailPanel.classList.add('hidden');
    currentDetailSender = '';
});

refreshBtn.addEventListener('click', loadInbox);

sendForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    
    const recipient = document.getElementById('recipient').value;
    const taskId = document.getElementById('task-id').value;
    const body = document.getElementById('body').value;
    
    submitBtn.disabled = true;
    
    try {
        const res = await fetch('/api/send', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                recipient: recipient,
                task_id: taskId,
                body: body
            })
        });
        
        if (!res.ok) {
            const errText = await res.text();
            throw new Error(errText);
        }
        const sentMsg = await res.json();
        sentMessages.push(sentMsg);
        if (sentMessages.length > 100) {
            sentMessages.splice(0, sentMessages.length - 100);
        }
        localStorage.setItem('sentMessages', JSON.stringify(sentMessages));
        
        showToast("Message sent successfully!");
        sendForm.reset();
        loadInbox();
    } catch (err) {
        showToast("Error: " + err.message);
    } finally {
        submitBtn.disabled = false;
    }
});

// Init
loadMe();
loadInbox();
setInterval(loadInbox, 10000);
