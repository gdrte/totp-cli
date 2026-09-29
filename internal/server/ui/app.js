"use strict";

const TOKEN_KEY = "totp-cli-api-token";
const rows = new Map(); // key -> { summary, el, code, expiresAt, fetching }
let mode = "secret";

const $ = (sel) => document.querySelector(sel);

function getToken() {
  try { return sessionStorage.getItem(TOKEN_KEY) || ""; } catch { return ""; }
}

function setToken(value) {
  try { sessionStorage.setItem(TOKEN_KEY, value); } catch { /* storage unavailable */ }
}

function showStatus(message, kind) {
  const el = $("#status");
  el.textContent = message;
  el.className = "status " + (kind || "");
  el.hidden = !message;
}

async function api(method, path, body) {
  const res = await fetch("/api/v1" + path, {
    method,
    headers: {
      "Authorization": "Bearer " + getToken(),
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  if (res.status === 204) return null;

  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || res.statusText);
    err.status = res.status;
    throw err;
  }
  return data;
}

function accountPath(s) {
  return "/accounts/" + encodeURIComponent(s.namespace) + "/" + encodeURIComponent(s.account);
}

async function loadAccounts() {
  if (!getToken()) {
    showStatus("Enter the API token printed by `totp-cli serve` to connect.");
    return;
  }

  let summaries;
  try {
    summaries = await api("GET", "/accounts");
  } catch (err) {
    showStatus(err.status === 401 ? "API token rejected." : "Failed to load accounts: " + err.message, "error");
    return;
  }

  if (!$("#status").classList.contains("ok")) showStatus("");

  const list = $("#accounts");
  list.replaceChildren();
  rows.clear();

  for (const s of summaries) {
    const el = $("#account-row").content.firstElementChild.cloneNode(true);
    el.querySelector(".ns").textContent = s.namespace;
    el.querySelector(".name").textContent = s.account;
    el.querySelector(".meta").textContent =
      `${s.algorithm} · ${s.length} digits · ${s.timePeriod}s` + (s.hasPrefix ? " · prefixed" : "");

    const key = s.namespace + "\u0000" + s.account;
    const row = { summary: s, el, code: "", expiresAt: 0, fetching: false };
    rows.set(key, row);

    el.querySelector(".code").addEventListener("click", () => copyCode(row));
    el.querySelector('[data-action="curl"]').addEventListener("click", () => copyCurl(s));
    el.querySelector('[data-action="delete"]').addEventListener("click", () => deleteAccount(s));

    list.append(el);
    refreshCode(row);
  }

  $("#empty").hidden = summaries.length > 0;
}

async function refreshCode(row) {
  if (row.fetching) return;
  row.fetching = true;
  try {
    const data = await api("GET", accountPath(row.summary) + "/code");
    row.code = data.code;
    row.expiresAt = Date.parse(data.expiresAt);
  } catch (err) {
    row.code = "error";
    row.expiresAt = Date.now() + 5000;
  } finally {
    row.fetching = false;
    render(row);
  }
}

function render(row) {
  const period = row.summary.timePeriod * 1000;
  const left = Math.max(0, row.expiresAt - Date.now());
  const codeEl = row.el.querySelector(".code");
  if (!codeEl.classList.contains("copied")) codeEl.textContent = row.code || "······";

  const timer = row.el.querySelector(".timer");
  timer.firstElementChild.style.width = Math.min(100, (left / period) * 100) + "%";
  timer.classList.toggle("low", left < 5000);
}

function tick() {
  for (const row of rows.values()) {
    if (row.expiresAt && Date.now() >= row.expiresAt) refreshCode(row);
    else render(row);
  }
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}

async function copyCode(row) {
  if (!row.code || row.code === "error") return;
  const el = row.el.querySelector(".code");
  if (await copyText(row.code)) {
    el.classList.add("copied");
    el.textContent = "copied";
    setTimeout(() => { el.classList.remove("copied"); render(row); }, 900);
  }
}

async function copyCurl(s) {
  const url = location.origin + "/api/v1" + accountPath(s) + "/code?min_remaining=5";
  const cmd = `curl -s -H "Authorization: Bearer $TOTP_API_TOKEN" '${url}'`;
  showStatus((await copyText(cmd)) ? "Copied: " + cmd : cmd, "ok");
}

async function deleteAccount(s) {
  if (!confirm(`Delete ${s.namespace}/${s.account}? This cannot be undone.`)) return;
  try {
    await api("DELETE", accountPath(s));
    showStatus(`Deleted ${s.namespace}/${s.account}.`, "ok");
    loadAccounts();
  } catch (err) {
    showStatus("Delete failed: " + err.message, "error");
  }
}

function setMode(next) {
  mode = next;
  for (const tab of document.querySelectorAll(".tab")) {
    tab.setAttribute("aria-selected", String(tab.dataset.mode === mode));
  }
  document.querySelector('[data-for="secret"]').hidden = mode !== "secret";
  document.querySelector('[data-for="uri"]').hidden = mode !== "uri";
}

async function addAccount(event) {
  event.preventDefault();
  const form = event.target;
  const f = new FormData(form);
  const body = {};

  for (const name of ["namespace", "account", "prefix", "algorithm"]) {
    const v = String(f.get(name) || "").trim();
    if (v) body[name] = v;
  }
  for (const name of ["length", "timePeriod"]) {
    const v = Number(f.get(name));
    if (v) body[name] = v;
  }
  const field = mode === "uri" ? "uri" : "secret";
  const v = String(f.get(field) || "").trim();
  if (v) body[field] = v;

  try {
    const created = await api("POST", "/accounts", body);
    showStatus(`Registered ${created.namespace}/${created.account}.`, "ok");
    form.reset();
    loadAccounts();
  } catch (err) {
    showStatus("Register failed: " + err.message, "error");
  }
}

document.addEventListener("DOMContentLoaded", () => {
  $("#api-token").value = getToken();

  $("#token-form").addEventListener("submit", (e) => {
    e.preventDefault();
    setToken($("#api-token").value.trim());
    loadAccounts();
  });

  for (const tab of document.querySelectorAll(".tab")) {
    tab.addEventListener("click", () => setMode(tab.dataset.mode));
  }

  $("#add-form").addEventListener("submit", addAccount);
  $("#refresh").addEventListener("click", loadAccounts);

  setInterval(tick, 1000);
  loadAccounts();
});
