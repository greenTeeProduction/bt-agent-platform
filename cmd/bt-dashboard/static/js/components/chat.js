/* === Chat Component === */

const agentNames = {
  overview: 'Admin Agent', thinktank: 'ThinkTank Moderator', company: 'Strategy Agent',
  tasks: 'PM Agent', trees: 'Tree Architect', mindmap: 'Viz Agent', evolution: 'Evolution Optimizer'
};

function toggleChat() {
  const panel = document.getElementById('chat-panel');
  panel.classList.toggle('open');
  document.getElementById('chat-agent-name').textContent =
    agentNames[state.activeTab] || 'BT Studio Assistant';
}

async function sendChat() {
  const input = document.getElementById('chat-input');
  const msg = input.value.trim();
  if (!msg) return;
  addChatMsg(msg, 'user');
  input.value = '';
  addChatMsg('Thinking...', 'thinking');
  try {
    const r = await apiFetch('/chat?msg=' + encodeURIComponent(msg) + '&tab=' + state.activeTab);
    const msgs = document.getElementById('chat-messages');
    // Remove thinking message
    msgs.lastElementChild.remove();
    addChatMsg(r.reply || 'No response', 'agent');
  } catch (e) {
    const msgs = document.getElementById('chat-messages');
    msgs.lastElementChild.remove();
    addChatMsg('Error: ' + e.message, 'agent');
  }
}

function addChatMsg(text, role) {
  const div = document.createElement('div');
  div.className = 'chat-msg ' + role;
  div.textContent = text;
  const msgs = document.getElementById('chat-messages');
  msgs.appendChild(div);
  msgs.scrollTop = msgs.scrollHeight;
}
