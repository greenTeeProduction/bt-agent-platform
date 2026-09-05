/* === Toast Notifications === */

function toast(msg, duration = 3000) {
  const existing = document.getElementById('toast');
  if (existing) existing.remove();

  const el = document.createElement('div');
  el.id = 'toast';
  el.className = 'toast';
  el.textContent = msg;
  document.body.appendChild(el);

  setTimeout(() => {
    if (el.parentNode) el.remove();
  }, duration);
}

window.toast = toast;
