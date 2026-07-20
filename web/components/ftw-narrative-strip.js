// <ftw-narrative-strip> — slim Live "NOW" story under ENERGY BALANCE.
//
// Fetches GET /api/narrative?window=now and renders Action / Restraint /
// Outlook as short English lines. Collapses when the body is empty.
// Does NOT show a SEK headline — savings owns Outcome.

import { FtwElement, ftwDebugDelay } from "./ftw-element.js";
import { apiFetch } from "./api-fetch.js";

class FtwNarrativeStrip extends FtwElement {
  static styles = `
    :host { display: block; }
    :host([hidden]) { display: none; }
    .strip {
      display: flex;
      flex-direction: column;
      gap: 6px;
      background: var(--ink-raised);
      border: 1px solid var(--line);
      border-radius: var(--radius-md, 10px);
      padding: var(--card-pad-tight, 12px 14px);
    }
    .head {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
    }
    .label {
      font-family: var(--mono);
      font-size: 10px;
      color: var(--fg-label);
      letter-spacing: 0.12em;
      text-transform: uppercase;
    }
    .label[data-register="hedged"] { color: var(--amber-d, var(--fg-muted)); }
    .link {
      font-family: var(--mono);
      font-size: 10px;
      letter-spacing: 0.06em;
      color: var(--fg-muted);
      text-decoration: none;
    }
    .link:hover { color: var(--accent-e); }
    .lines {
      display: flex;
      flex-direction: column;
      gap: 4px;
    }
    .line {
      font-family: var(--sans);
      font-size: 0.88rem;
      line-height: 1.35;
      color: var(--fg);
      margin: 0;
    }
    .line.restraint { color: var(--fg-dim); }
    .line.outlook { color: var(--cyan-dim, var(--fg-dim)); }
    .empty {
      font-family: var(--sans);
      font-size: 0.85rem;
      color: var(--fg-muted);
    }
  `;

  static get observedAttributes() {
    return ["poll-ms"];
  }

  constructor() {
    super();
    this._timer = null;
    this._abort = null;
    this._payload = null;
    this._state = "loading";
  }

  connectedCallback() {
    super.connectedCallback();
    this._refresh();
    this._restartPolling();
  }

  disconnectedCallback() {
    if (this._timer) { clearInterval(this._timer); this._timer = null; }
    if (this._abort) { this._abort.abort(); this._abort = null; }
  }

  attributeChangedCallback(name) {
    if (name === "poll-ms") {
      this._restartPolling();
    }
  }

  _restartPolling() {
    if (this._timer) { clearInterval(this._timer); this._timer = null; }
    const raw = this.getAttribute("poll-ms");
    const ms = Number(raw ?? 20000);
    if (ms > 0 && this.isConnected) {
      this._timer = setInterval(() => this._refresh(), ms);
    }
  }

  async _refresh() {
    if (this._abort) this._abort.abort();
    this._abort = new AbortController();
    const delay = ftwDebugDelay();
    try {
      if (delay) await new Promise((r) => setTimeout(r, delay));
      const res = await apiFetch("/api/narrative?window=now", {
        signal: this._abort.signal,
      });
      if (!res.ok) throw new Error("narrative " + res.status);
      this._payload = await res.json();
      this._state = "ready";
    } catch (e) {
      if (e && e.name === "AbortError") return;
      this._state = "error";
      this._payload = null;
    }
    this.update();
  }

  render() {
    if (this._state === "loading") {
      return `<div class="strip"><div class="empty">Reading the house…</div></div>`;
    }
    if (this._state === "error" || !this._payload) {
      return `<div class="strip"><div class="empty">Story unavailable</div></div>`;
    }
    const p = this._payload;
    const action = (p.action || "").trim();
    const restraint = (p.restraint || "").trim();
    const outlook = (p.outlook || "").trim();
    if (!action && !restraint && !outlook) {
      return `<div class="strip"><div class="empty">Holding steady</div></div>`;
    }
    const reg = p.register === "hedged" ? "hedged" : "assertive";
    const lines = [];
    if (action) lines.push(`<p class="line action">${esc(action)}</p>`);
    if (restraint) lines.push(`<p class="line restraint">${esc(restraint)}</p>`);
    if (outlook) lines.push(`<p class="line outlook">${esc(outlook)}</p>`);
    return `
      <div class="strip">
        <div class="head">
          <div class="label" data-register="${reg}">Now</div>
          <a class="link" href="#live-plan-row">View plan →</a>
        </div>
        <div class="lines">${lines.join("")}</div>
      </div>
    `;
  }
}

function esc(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

customElements.define("ftw-narrative-strip", FtwNarrativeStrip);
