// DoorMate Page-First AI Assistant Tab Controller & Rendering Library
// Globally loaded sequential script. No ES module import or export.

let activePageID = null;

function renderDoormate() {
  return `
    <div class="doormate-layout">
      <!-- Left Column: Intent Canvas & Interactive Loop -->
      <div class="intent-column">
        <div class="intent-canvas-box">
          <div class="intent-canvas-bg" id="doormate-canvas-bg"></div>
          <div class="intent-canvas-content">
            <h2 class="canvas-title">DoorMate Gateway Assistant</h2>
            <p class="canvas-subtitle">Personalized AI Page Design</p>
            
            <!-- Voice & Video Interface Indicator -->
            <div class="av-interface-row">
              <button class="av-btn" id="doormate-mic-btn" title="Toggle Voice Interaction">
                <span class="av-icon">🎙️</span>
                <span class="av-text" id="mic-status">Voice Idle</span>
              </button>
              <button class="av-btn" id="doormate-cam-btn" title="Toggle Video Calibration">
                <span class="av-icon">📷</span>
                <span class="av-text" id="cam-status">Video Idle</span>
              </button>
            </div>

            <!-- Predicted Interactive Bubbles -->
            <div class="bubble-container" id="predicted-bubbles">
              <span class="bubble-placeholder-text">Enter a request to bootstrap your gateway.</span>
            </div>

            <!-- Input / Phrase Area (WhatsApp Style Send) -->
            <div class="whatsapp-send-bar">
              <input type="text" id="doormate-input" placeholder="Type a concept, design style, or security standard..." />
              <button id="doormate-send-btn" title="Send Request">
                <svg viewBox="0 0 24 24" width="20" height="20">
                  <path fill="currentColor" d="M2,21L23,12L2,3V10L17,12L2,14V21Z" />
                </svg>
              </button>
            </div>
          </div>
        </div>

        <!-- Personalization & Personal Profile Tags -->
        <div class="profile-box">
          <h3>Your Personalization Signals</h3>
          <div class="profile-tags-container" id="doormate-profile-tags">
            <!-- Dynamically populated preference tags -->
          </div>
        </div>
      </div>

      <!-- Right Column: Structured Generated Page Workspace -->
      <div class="page-workspace" id="doormate-workspace">
        <div class="workspace-empty-state">
          <div class="empty-icon">🚪</div>
          <h4>No Generated Page Blueprint Active</h4>
          <p>Provide a gateway phrase or click an interactive bubble on the left to dynamically compile a structured web page response.</p>
        </div>
      </div>
    </div>
  `;
}

function initDoormateTab() {
  const sendBtn = document.getElementById('doormate-send-btn');
  const inputEl = document.getElementById('doormate-input');
  if (!sendBtn || !inputEl) return;

  sendBtn.addEventListener('click', () => sendPhrase());
  inputEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') sendPhrase();
  });

  // AV Simulation
  const micBtn = document.getElementById('doormate-mic-btn');
  const camBtn = document.getElementById('doormate-cam-btn');
  let micActive = false;
  let camActive = false;

  if (micBtn && camBtn) {
    micBtn.addEventListener('click', () => {
      micActive = !micActive;
      micBtn.classList.toggle('active', micActive);
      document.getElementById('mic-status').textContent = micActive ? "Listening..." : "Voice Idle";
    });

    camBtn.addEventListener('click', () => {
      camActive = !camActive;
      camBtn.classList.toggle('active', camActive);
      document.getElementById('cam-status').textContent = camActive ? "Calibrating..." : "Video Idle";
    });
  }

  // Initial Profile Load
  loadProfile();
}

async function loadProfile() {
  try {
    const res = await apiFetch('/doormate/profile');
    if (res) renderProfileTags(res.preference_tags || []);
  } catch (err) {
    console.error('load profile error', err);
  }
}

function renderProfileTags(tags) {
  const container = document.getElementById('doormate-profile-tags');
  if (!container) return;
  if (tags.length === 0) {
    container.innerHTML = `<span class="no-tags-notice">No signals learned yet. Submit security or design intents to bootstrap learning.</span>`;
    return;
  }
  container.innerHTML = tags.map(tag => `<span class="profile-tag-chip">✓ ${tag}</span>`).join('');
}

async function sendPhrase(customText = null) {
  const inputEl = document.getElementById('doormate-input');
  const text = customText || (inputEl ? inputEl.value.trim() : "");
  if (!text) return;

  if (!customText && inputEl) inputEl.value = '';

  const workspace = document.getElementById('doormate-workspace');
  if (workspace) {
    workspace.innerHTML = `
      <div class="workspace-loading">
        <div class="spinner"></div>
        <p>Compiling structured page from template library...</p>
      </div>
    `;
  }

  try {
    const res = await apiFetch('/doormate/intent', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ input: text })
    });

    if (res && res.page) {
      activePageID = res.page.id;
      renderBubbles(res.bubbles || []);
      renderProfileTags(res.profile ? res.profile.preference_tags : []);
      renderGeneratedPage(res.page);
    } else {
      if (workspace) workspace.innerHTML = `<div class="render-error">Error compiling template.</div>`;
    }
  } catch (err) {
    if (workspace) workspace.innerHTML = `<div class="render-error">Service exception: ${err.message}</div>`;
  }
}

function renderBubbles(bubbles) {
  const container = document.getElementById('predicted-bubbles');
  if (!container) return;
  container.innerHTML = bubbles.map(b => `
    <button class="predicted-bubble-btn" data-bubble="${b}">
      ${b}
    </button>
  `).join('');

  // Register bubble click actions to simulate sequential refinement
  container.querySelectorAll('.predicted-bubble-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      sendPhrase(btn.getAttribute('data-bubble'));
    });
  });
}

// Complete Page Template Rendering Library
function renderGeneratedPage(page) {
  const container = document.getElementById('doormate-workspace');
  if (!container) return;

  const schema = page.schema;
  const isBookmarked = page.bookmarked;
  const rating = page.rating || 0;

  let pageContent = `
    <div class="generated-page-container theme-${schema.template_id}">
      <!-- Page Header & Action Controls -->
      <div class="generated-page-header">
        <div class="title-section">
          <span class="template-label-badge">${schema.template_id.toUpperCase()} TEMPLATE</span>
          <h2>${schema.title}</h2>
          <p class="summary-paragraph">${schema.summary}</p>
        </div>
        <div class="header-action-controls">
          <button class="page-action-btn ${isBookmarked ? 'active' : ''}" id="btn-bookmark" title="Bookmark Board">
            ${isBookmarked ? '★ Bookmarked' : '☆ Bookmark'}
          </button>
          
          <!-- Rating System -->
          <div class="page-rating-row">
            ${[1, 2, 3, 4, 5].map(star => `
              <span class="star-rating-icon ${star <= rating ? 'active' : ''}" data-star="${star}">★</span>
            `).join('')}
          </div>
        </div>
      </div>

      <!-- Dynamic Content Blocks Section -->
      <div class="generated-page-body">
        ${schema.blocks.map(block => renderBlock(block)).join('')}
      </div>

      <!-- Follow-up Bubble Actions (Implicit Rail) -->
      ${schema.follow_ups && schema.follow_ups.length > 0 ? `
        <div class="follow-up-actions-rail">
          <h4>Continuous Interaction Follow-ups:</h4>
          <div class="follow-up-bubbles-row">
            ${schema.follow_ups.map(f => `
              <button class="follow-up-bubble-pill" data-phrase="${f}">
                ➔ ${f}
              </button>
            `).join('')}
          </div>
        </div>
      ` : ''}
    </div>
  `;

  container.innerHTML = pageContent;

  // Setup interaction event handlers
  document.getElementById('btn-bookmark').addEventListener('click', () => toggleBookmark());
  container.querySelectorAll('.star-rating-icon').forEach(icon => {
    icon.addEventListener('click', () => {
      const star = parseInt(icon.getAttribute('data-star'));
      ratePage(star);
    });
  });
  container.querySelectorAll('.follow-up-bubble-pill').forEach(pill => {
    pill.addEventListener('click', () => {
      sendPhrase(pill.getAttribute('data-phrase'));
    });
  });

  // Dynamic canvas-bg change matching template choice
  const canvasBg = document.getElementById('doormate-canvas-bg');
  if (canvasBg) {
    if (schema.template_id === 'recommendation') {
      canvasBg.className = "intent-canvas-bg bg-security";
    } else if (schema.template_id === 'guide') {
      canvasBg.className = "intent-canvas-bg bg-design";
    } else {
      canvasBg.className = "intent-canvas-bg bg-general";
    }
  }
}

// Block Renderer matching standard schemas
function renderBlock(block) {
  switch (block.type) {
    case 'overview':
      return `
        <div class="rendered-block block-overview">
          <h3>${block.title || 'Summary Details'}</h3>
          <p>${block.content || ''}</p>
        </div>
      `;
    case 'cards':
    case 'gallery':
      return `
        <div class="rendered-block block-cards">
          <h3>${block.title || 'Options Blueprint'}</h3>
          <div class="cards-grid">
            ${(block.items || []).map(item => `
              <div class="ui-blueprint-card">
                <div class="card-indicator-node"></div>
                <p>${item}</p>
              </div>
            `).join('')}
          </div>
        </div>
      `;
    case 'list':
      return `
        <div class="rendered-block block-list">
          <h3>${block.title || 'Implementation Tasks'}</h3>
          <ul class="blueprint-list">
            ${(block.items || []).map(item => `<li><span class="bullet-indicator">▪</span> ${item}</li>`).join('')}
          </ul>
        </div>
      `;
    case 'comparison':
      return `
        <div class="rendered-block block-comparison">
          <h3>${block.title || 'Structured Comparison Matrix'}</h3>
          <div class="comparison-table-wrapper">
            <table>
              <thead>
                <tr>
                  ${(block.headers || []).map(h => `<th>${h}</th>`).join('')}
                </tr>
              </thead>
              <tbody>
                ${(block.rows || []).map(row => `
                  <tr>
                    ${row.map(cell => `<td>${cell}</td>`).join('')}
                  </tr>
                `).join('')}
              </tbody>
            </table>
          </div>
        </div>
      `;
    case 'chart':
      const maxVal = Math.max(...(block.data_points || []).map(dp => dp.value), 100);
      return `
        <div class="rendered-block block-chart">
          <h3>${block.title || 'Performance Metrics'}</h3>
          <div class="svg-chart-container">
            <svg viewBox="0 0 500 200" width="100%" height="100%">
              <!-- Bar chart -->
              ${(block.data_points || []).map((dp, i) => {
                const x = 50 + (i * 110);
                const height = (dp.value / maxVal) * 140;
                const y = 170 - height;
                return `
                  <rect x="${x}" y="${y}" width="60" height="${height}" rx="4" fill="var(--accent)" />
                  <text x="${x + 30}" y="190" text-anchor="middle" font-size="10" fill="var(--text-secondary)">${dp.label}</text>
                  <text x="${x + 30}" y="${y - 8}" text-anchor="middle" font-size="11" font-weight="bold" fill="var(--text-primary)">${dp.value}%</text>
                `;
              }).join('')}
              <line x1="30" y1="170" x2="480" y2="170" stroke="var(--border-standard)" stroke-width="2" />
            </svg>
          </div>
        </div>
      `;
    case 'diagram':
      return `
        <div class="rendered-block block-diagram">
          <h3>${block.title || 'Process Workflow Logic'}</h3>
          <div class="svg-diagram-container">
            <svg viewBox="0 0 600 150" width="100%" height="100%">
              <!-- Quick horizontal node-link flow -->
              ${(block.nodes || []).map((node, i) => {
                const x = 60 + (i * 120);
                const y = 60;
                let color = "var(--bg-surface)";
                let stroke = "var(--border-standard)";
                if (node.type === 'start') { color = "rgba(40,167,69,0.15)"; stroke = "#27a644"; }
                else if (node.type === 'decision') { color = "rgba(255,193,7,0.15)"; stroke = "#f59e0b"; }
                else if (node.type === 'end') { color = "rgba(0,123,255,0.15)"; stroke = "#3b82f6"; }
                return `
                  <rect x="${x}" y="${y}" width="90" height="40" rx="6" fill="${color}" stroke="${stroke}" stroke-width="2"/>
                  <text x="${x + 45}" y="${y + 24}" text-anchor="middle" font-size="9" fill="var(--text-primary)">${node.label}</text>
                `;
              }).join('')}
              
              <!-- Draw simplistic link lines between sequential elements -->
              ${(block.edges || []).map((edge, i) => {
                // Find node indices
                const fromIndex = (block.nodes || []).findIndex(n => n.id === edge.from);
                const toIndex = (block.nodes || []).findIndex(n => n.id === edge.to);
                if (fromIndex === -1 || toIndex === -1) return '';
                const x1 = 150 + (fromIndex * 120);
                const x2 = 60 + (toIndex * 120);
                const y = 80;
                return `
                  <line x1="${x1}" y1="${y}" x2="${x2}" y2="${y}" stroke="var(--text-tertiary)" stroke-width="1.5" stroke-dasharray="4,4" />
                  <polygon points="${x2},${y} ${x2-5},${y-3} ${x2-5},${y+3}" fill="var(--text-tertiary)"/>
                  ${edge.label ? `<text x="${(x1+x2)/2}" y="${y - 4}" text-anchor="middle" font-size="8" fill="var(--text-tertiary)">${edge.label}</text>` : ''}
                `;
              }).join('')}
            </svg>
          </div>
        </div>
      `;
    default:
      return '';
  }
}

async function toggleBookmark() {
  if (!activePageID) return;
  try {
    const res = await apiFetch('/doormate/bookmark', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ page_id: activePageID })
    });
    if (res && res.status === "success") {
      const btn = document.getElementById('btn-bookmark');
      if (res.bookmarked) {
        btn.classList.add('active');
        btn.textContent = '★ Bookmarked';
      } else {
        btn.classList.remove('active');
        btn.textContent = '☆ Bookmark';
      }
    }
  } catch (err) {
    console.error('bookmark toggle failed', err);
  }
}

async function ratePage(starCount) {
  if (!activePageID) return;
  try {
    const res = await apiFetch('/doormate/rate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ page_id: activePageID, rating: starCount })
    });
    if (res && res.status === "success") {
      const starIcons = document.querySelectorAll('.star-rating-icon');
      starIcons.forEach(icon => {
        const index = parseInt(icon.getAttribute('data-star'));
        if (index <= starCount) {
          icon.classList.add('active');
        } else {
          icon.classList.remove('active');
        }
      });
    }
  } catch (err) {
    console.error('rating submission failed', err);
  }
}
