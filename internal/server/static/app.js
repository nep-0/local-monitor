const rows = document.querySelector("#device-rows");
const summary = document.querySelector("#summary");
const lastUpdated = document.querySelector("#last-updated");
const totalCount = document.querySelector("#total-count");
const onlineCount = document.querySelector("#online-count");
const offlineCount = document.querySelector("#offline-count");
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
