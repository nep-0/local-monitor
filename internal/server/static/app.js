const rows = document.querySelector("#device-rows");
const summary = document.querySelector("#summary");
const lastUpdated = document.querySelector("#last-updated");
const totalCount = document.querySelector("#total-count");
const onlineCount = document.querySelector("#online-count");
const offlineCount = document.querySelector("#offline-count");
const timelineRange = document.querySelector("#timeline-range");
const timelineList = document.querySelector("#timeline-list");
const historyTitle = document.querySelector("#history-title");
const historyList = document.querySelector("#history-list");
const refreshButton = document.querySelector("#refresh");
const probeButton = document.querySelector("#probe");

let selectedIP = "";

refreshButton.addEventListener("click", loadStatuses);
probeButton.addEventListener("click", runProbe);

async function loadStatuses() {
  setBusy(refreshButton, true);
  try {
    const devices = await fetchJSON("/api/statuses");
    renderStatuses(devices);
    await loadTimeline();
  } catch (error) {
    showError(error.message);
  } finally {
    setBusy(refreshButton, false);
  }
}

async function runProbe() {
  setBusy(probeButton, true);
  try {
    await fetchJSON("/api/probe", { method: "POST" });
    await loadStatuses();
    if (selectedIP) {
      await loadHistory(selectedIP);
    }
  } catch (error) {
    showError(error.message);
  } finally {
    setBusy(probeButton, false);
  }
}

async function loadTimeline() {
  const timeline = await fetchJSON("/api/timeline?days=7");
  renderTimeline(timeline);
}

async function loadHistory(ip) {
  selectedIP = ip;
  historyTitle.textContent = `History: ${ip}`;
  historyList.innerHTML = `<div class="empty">Loading history...</div>`;
  try {
    const history = await fetchJSON(`/api/devices/${encodeURIComponent(ip)}/history?limit=20`);
    renderHistory(history);
  } catch (error) {
    historyList.innerHTML = `<div class="empty">${escapeHTML(error.message)}</div>`;
  }
}

function renderStatuses(devices) {
  const online = devices.filter((device) => device.Online).length;
  const offline = devices.length - online;

  totalCount.textContent = devices.length;
  onlineCount.textContent = online;
  offlineCount.textContent = offline;
  summary.textContent = `${online} online, ${offline} offline`;
  lastUpdated.textContent = `Updated ${formatDate(new Date().toISOString())}`;

  if (devices.length === 0) {
    rows.innerHTML = `<tr><td class="muted" colspan="6">No devices configured</td></tr>`;
    return;
  }

  rows.innerHTML = devices.map((device) => `
    <tr data-ip="${escapeHTML(device.IP)}">
      <td>${escapeHTML(device.Name)}</td>
      <td>${escapeHTML(device.IP)}</td>
      <td>${escapeHTML(device.Group || "Ungrouped")}</td>
      <td><span class="status ${device.Online ? "online" : "offline"}">${device.Online ? "Online" : "Offline"}</span></td>
      <td>${formatDate(device.LastSeen)}</td>
      <td>${formatDate(device.CheckedAt)}</td>
    </tr>
  `).join("");

  document.querySelectorAll("tr[data-ip]").forEach((row) => {
    row.addEventListener("click", () => loadHistory(row.dataset.ip));
  });
}

function renderTimeline(timeline) {
  timelineRange.textContent = `${formatShortDate(timeline.since)} - ${formatShortDate(timeline.until)}`;
  if (timeline.devices.length === 0) {
    timelineList.innerHTML = `<div class="empty">No timeline data available.</div>`;
    return;
  }

  const since = new Date(timeline.since).getTime();
  const until = new Date(timeline.until).getTime();
  const span = Math.max(until - since, 1);

  timelineList.innerHTML = timeline.devices.map((device) => {
    const blocks = device.entries.map((entry) => {
      const start = clamp(((new Date(entry.start).getTime() - since) / span) * 100, 0, 100);
      const end = clamp(((new Date(entry.end).getTime() - since) / span) * 100, 0, 100);
      const width = Math.max(end - start, 0.4);
      const title = `${entry.online ? "Online" : "Offline"} from ${formatDate(entry.start)} to ${formatDate(entry.end)}`;
      return `<rect x="${start.toFixed(3)}" y="5" width="${width.toFixed(3)}" height="18" rx="2" class="${entry.online ? "online-block" : "offline-block"}"><title>${escapeHTML(title)}</title></rect>`;
    }).join("");

    return `
      <div class="timeline-row">
        <div>
          <strong>${escapeHTML(device.name)}</strong>
          <span>${escapeHTML(device.ip)}</span>
        </div>
        ${renderTimelineSVG(device, blocks)}
      </div>
    `;
  }).join("");
}

function renderTimelineSVG(device, blocks) {
  if (!blocks) {
    return `<div class="timeline-track"><span class="timeline-empty">No entries</span></div>`;
  }
  return `
    <svg class="timeline-svg" viewBox="0 0 100 28" preserveAspectRatio="none" role="img" aria-label="${escapeHTML(device.name)} 7-day status timeline">
      <rect x="0" y="5" width="100" height="18" rx="2" class="unknown-block"></rect>
      ${blocks}
    </svg>
  `;
}

function renderHistory(history) {
  if (history.length === 0) {
    historyList.innerHTML = `<div class="empty">No status history recorded for this device.</div>`;
    return;
  }

  historyList.innerHTML = history.map((item) => `
    <div class="history-item">
      <span class="status ${item.Online ? "online" : "offline"}">${item.Online ? "Online" : "Offline"}</span>
      <span>${formatDate(item.CheckedAt)}</span>
    </div>
  `).join("");
}

async function fetchJSON(url, options = {}) {
  const response = await fetch(url, options);
  const payload = await response.json();
  if (!response.ok) {
    throw new Error(payload.error || `Request failed with ${response.status}`);
  }
  return payload;
}

function formatDate(value) {
  if (!value || value.startsWith("0001-01-01")) {
    return "Never";
  }
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(new Date(value));
}

function formatShortDate(value) {
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
  }).format(new Date(value));
}

function clamp(value, min, max) {
  return Math.min(Math.max(value, min), max);
}

function setBusy(button, busy) {
  button.disabled = busy;
}

function showError(message) {
  summary.textContent = message;
}

function escapeHTML(value) {
  return String(value).replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    "\"": "&quot;",
    "'": "&#39;",
  })[char]);
}

loadStatuses();
setInterval(loadStatuses, 15000);
