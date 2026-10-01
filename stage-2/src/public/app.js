(function () {
  "use strict";

  // ===== tiny DOM helpers ====================================================

  function el(tag, attrs, children) {
    const node = document.createElement(tag);
    if (attrs) {
      for (const k in attrs) {
        if (!Object.prototype.hasOwnProperty.call(attrs, k)) continue;
        const v = attrs[k];
        if (k === "text") node.textContent = v;
        else if (k.indexOf("on") === 0 && typeof v === "function") node.addEventListener(k.slice(2), v);
        else if (v !== undefined && v !== null) node.setAttribute(k, v);
      }
    }
    if (children) {
      for (const c of [].concat(children)) if (c) node.appendChild(c);
    }
    return node;
  }

  function slot(name) {
    return document.querySelector('[data-slot="' + name + '"]');
  }

  function clearSlot(name) {
    const s = slot(name);
    if (s) s.innerHTML = "";
  }

  function q(testid) {
    return document.querySelector('[data-testid="' + testid + '"]');
  }

  // ===== auth session (persists across route navigations; not required to
  // survive a hard reload of in-flight page state, only the signed-in session) ==

  const AUTH_KEY = "tk_auth";

  function getAuth() {
    try {
      const raw = localStorage.getItem(AUTH_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch (_e) {
      return null;
    }
  }

  function setAuth(a) {
    localStorage.setItem(AUTH_KEY, JSON.stringify(a));
    renderAuthArea();
  }

  function clearAuth() {
    localStorage.removeItem(AUTH_KEY);
    renderAuthArea();
  }

  function renderAuthArea() {
    const area = slot("auth-area");
    if (!area) return;
    area.innerHTML = "";
    const auth = getAuth();
    if (auth && auth.token) {
      area.appendChild(el("span", { "data-testid": "current-user", class: "current-user", text: auth.display_name }));
      area.appendChild(el("button", {
        type: "button", "data-testid": "logout-button", class: "btn btn-ghost", text: "Sign out",
        onclick: function () { clearAuth(); },
      }));
    } else {
      area.appendChild(el("a", { href: "/login", class: "btn btn-ghost", "data-auth-link": "login", text: "Sign in" }));
      area.appendChild(el("a", { href: "/signup", class: "btn btn-primary", "data-auth-link": "signup", text: "Sign up" }));
    }
  }

  // ===== API client ===========================================================

  const FETCH_TIMEOUT_MS = 8000;

  function NetworkFailure(message) {
    this.message = message;
    this.name = "NetworkFailure";
  }
  NetworkFailure.prototype = Object.create(Error.prototype);

  async function apiCall(path, opts) {
    opts = opts || {};
    let url = path;
    if (opts.params) {
      const qs = new URLSearchParams();
      for (const k in opts.params) {
        if (opts.params[k] !== undefined && opts.params[k] !== null) qs.set(k, opts.params[k]);
      }
      const s = qs.toString();
      if (s) url += "?" + s;
    }
    const headers = {};
    if (opts.json !== undefined) headers["Content-Type"] = "application/json";
    if (opts.auth !== false) {
      const a = getAuth();
      if (a && a.token) headers["Authorization"] = "Bearer " + a.token;
    }
    if (opts.idempotencyKey) headers["Idempotency-Key"] = opts.idempotencyKey;

    const controller = new AbortController();
    const timer = setTimeout(function () { controller.abort(); }, FETCH_TIMEOUT_MS);
    let resp;
    try {
      resp = await fetch(url, {
        method: opts.method || "GET",
        headers: headers,
        body: opts.json !== undefined ? JSON.stringify(opts.json) : undefined,
        signal: controller.signal,
      });
    } catch (e) {
      // Covers a dropped connection, a refused connection and our own
      // client-side timeout alike: from here, the outcome is genuinely
      // unknown, which is exactly the "uncertain" case the spec describes.
      throw new NetworkFailure((e && e.message) || "network failure");
    } finally {
      clearTimeout(timer);
    }
    let body = null;
    const text = await resp.text();
    if (text) {
      try { body = JSON.parse(text); } catch (_e) { body = null; }
    }
    return { status: resp.status, ok: resp.ok, body: body };
  }

  function errorMessage(resp, fallback) {
    return (resp && resp.body && resp.body.error && resp.body.error.message) || fallback;
  }

  function newIdempotencyKey() {
    if (window.crypto && window.crypto.randomUUID) return window.crypto.randomUUID();
    return "k-" + Date.now() + "-" + Math.random().toString(16).slice(2);
  }

  // ===== shared: a conditional error paragraph in a named slot ===============

  function showErrorIn(slotName, testid, message) {
    const s = slot(slotName);
    if (!s) return;
    s.innerHTML = "";
    s.appendChild(el("p", { "data-testid": testid, class: "error-text", role: "alert", text: message }));
  }

  // ===== bootstrap =============================================================

  document.addEventListener("DOMContentLoaded", function () {
    renderAuthArea();
    const page = document.body.dataset.page;
    if (page === "search") initSearchPage();
    else if (page === "signup") initSignupPage();
    else if (page === "login") initLoginPage();
    else if (page === "lookup") initLookupPage();
  });

  // ===== /signup ===============================================================

  function initSignupPage() {
    const form = q("signup-form");
    form.addEventListener("submit", async function (ev) {
      ev.preventDefault();
      clearSlot("auth-error");
      const email = q("signup-email").value;
      const password = q("signup-password").value;
      const display_name = q("signup-display-name").value;
      try {
        const resp = await apiCall("/auth/signup", {
          method: "POST", auth: false, json: { email: email, password: password, display_name: display_name },
        });
        if (resp.ok) {
          setAuth({ user_id: resp.body.user_id, display_name: resp.body.display_name, token: resp.body.token });
        } else {
          showErrorIn("auth-error", "auth-error", errorMessage(resp, "Sign up failed."));
        }
      } catch (_e) {
        showErrorIn("auth-error", "auth-error", "Could not reach the server. Please try again.");
      }
    });
  }

  // ===== /login =================================================================

  function initLoginPage() {
    const form = q("login-form");
    form.addEventListener("submit", async function (ev) {
      ev.preventDefault();
      clearSlot("auth-error");
      const email = q("login-email").value;
      const password = q("login-password").value;
      try {
        const resp = await apiCall("/auth/login", {
          method: "POST", auth: false, json: { email: email, password: password },
        });
        if (resp.ok) {
          setAuth({ user_id: resp.body.user_id, display_name: resp.body.display_name, token: resp.body.token });
        } else {
          showErrorIn("auth-error", "auth-error", errorMessage(resp, "Sign in failed."));
        }
      } catch (_e) {
        showErrorIn("auth-error", "auth-error", "Could not reach the server. Please try again.");
      }
    });
  }

  // ===== / (search, grid, booking, confirmation) ===============================

  function initSearchPage() {
    const restaurantSelect = q("restaurant-select");
    const dateInput = q("date-input");
    const partyInput = q("party-size-input");
    const form = q("search-form");

    const restaurantDetailCache = {};
    let searchSeq = 0;     // guards against a late/out-of-order response winning
    let current = null;    // the latest APPLIED search: {restaurantId, date, partySize, restaurant, slots}
    let attempt = null;    // {key, body, ids, hhmm, slotObj} for the open booking form

    try {
      dateInput.value = new Date().toISOString().slice(0, 10);
    } catch (_e) { /* cosmetic default only */ }

    async function loadRestaurants() {
      try {
        const resp = await apiCall("/restaurants", { auth: false });
        if (resp.ok) {
          restaurantSelect.innerHTML = "";
          for (const r of resp.body.restaurants) {
            restaurantSelect.appendChild(el("option", { value: r.id, text: r.name }));
          }
        }
      } catch (_e) { /* best effort; the select just stays empty */ }
    }

    async function restaurantDetail(id) {
      if (restaurantDetailCache[id]) return restaurantDetailCache[id];
      const resp = await apiCall("/restaurants/" + encodeURIComponent(id), { auth: false });
      if (resp.ok) { restaurantDetailCache[id] = resp.body; return resp.body; }
      return null;
    }

    function tableLabel(id) {
      const t = current.restaurant.tables.find(function (x) { return x.id === id; });
      return t ? t.label : id;
    }

    function capacityOf(ids) {
      return ids.reduce(function (sum, id) {
        const t = current.restaurant.tables.find(function (x) { return x.id === id; });
        return sum + (t ? t.capacity : 0);
      }, 0);
    }

    function optionAvailable(slotObj, ids) {
      const key = ids.join(" ");
      return (slotObj.available_options || []).some(function (o) {
        return o.table_ids.join(" ") === key;
      });
    }

    function resetInteraction() {
      clearSlot("booking");
      clearSlot("confirmation");
      attempt = null;
    }

    form.addEventListener("submit", async function (ev) {
      ev.preventDefault();
      resetInteraction();
      clearSlot("search-auth-error");
      const restaurantId = restaurantSelect.value;
      const date = dateInput.value;
      const partySize = parseInt(partyInput.value, 10);
      const mySeq = ++searchSeq;

      const resultsSlot = slot("results");
      resultsSlot.innerHTML = "";
      resultsSlot.appendChild(el("p", { class: "loading-state", role: "status", text: "Searching…" }));

      let detail, availResp;
      try {
        const pair = await Promise.all([
          restaurantDetail(restaurantId),
          apiCall("/availability", { auth: false, params: { restaurant_id: restaurantId, date: date, party_size: partySize } }),
        ]);
        detail = pair[0];
        availResp = pair[1];
      } catch (_e) {
        if (mySeq !== searchSeq) return; // a newer search already supersedes this failure
        resultsSlot.innerHTML = "";
        resultsSlot.appendChild(el("p", { class: "error-text", role: "alert", text: "Could not load availability. Please try again." }));
        return;
      }
      if (mySeq !== searchSeq) return; // stale: a newer search finished first; never let this one win

      if (!availResp.ok || !detail) {
        resultsSlot.innerHTML = "";
        resultsSlot.appendChild(el("p", { class: "error-text", role: "alert", text: errorMessage(availResp, "Search failed.") }));
        return;
      }

      current = { restaurantId: restaurantId, date: date, partySize: partySize, restaurant: detail, slots: availResp.body.slots };
      renderGrid();
    });

    function renderGrid() {
      const resultsSlot = slot("results");
      resultsSlot.innerHTML = "";
      if (!current.slots.length) {
        resultsSlot.appendChild(el("p", { "data-testid": "no-slots", class: "empty-state",
          text: "No tables are available that day. Try another date or party size." }));
        return;
      }
      const singles = current.restaurant.tables.map(function (t) {
        return { ids: [t.id], label: "Table " + t.label };
      });
      const pairs = (current.restaurant.combinable || []).map(function (pair) {
        return { ids: [pair[0], pair[1]], label: "Tables " + tableLabel(pair[0]) + " + " + tableLabel(pair[1]) };
      });
      const columns = singles.concat(pairs);

      const table = el("table", { "data-testid": "availability-grid", class: "availability-grid" });
      const thead = el("thead");
      const headRow = el("tr");
      headRow.appendChild(el("th", { scope: "col", text: "Time" }));
      for (const col of columns) headRow.appendChild(el("th", { scope: "col", text: col.label }));
      thead.appendChild(headRow);
      table.appendChild(thead);

      const tbody = el("tbody");
      for (const s of current.slots) {
        const hhmm = s.starts_at_local.split("T")[1];
        const row = el("tr");
        row.appendChild(el("th", { scope: "row", text: hhmm }));
        for (const col of columns) {
          const key = col.ids.join("+");
          const available = optionAvailable(s, col.ids);
          const btn = el("button", {
            type: "button",
            "data-testid": "slot-" + key + "-" + hhmm,
            "data-available": available ? "true" : "false",
            "aria-label": col.label + " at " + hhmm + (available ? ", available" : ", unavailable"),
            class: "slot-cell" + (available ? " slot-available" : " slot-unavailable"),
            text: available ? "Available" : "—",
          });
          if (!available) btn.disabled = true;
          btn.addEventListener("click", (function (ids, hhmm, s) {
            return function () { onCellClick(ids, hhmm, s); };
          })(col.ids, hhmm, s));
          const td = el("td");
          td.appendChild(btn);
          row.appendChild(td);
        }
        tbody.appendChild(row);
      }
      table.appendChild(tbody);
      const scroller = el("div", { class: "grid-scroll" });
      scroller.appendChild(table);
      resultsSlot.appendChild(scroller);
    }

    function onCellClick(ids, hhmm, slotObj) {
      const auth = getAuth();
      if (!auth || !auth.token) {
        showErrorIn("search-auth-error", "auth-error", "Sign in to book a table.");
        return;
      }
      if (!optionAvailable(slotObj, ids)) return; // defense in depth; disabled cells already refuse clicks
      openBookingForm(ids, hhmm, slotObj);
    }

    function openBookingForm(ids, hhmm, slotObj) {
      attempt = null;
      clearSlot("confirmation");
      const bookingSlot = slot("booking");
      bookingSlot.innerHTML = "";

      const tableLabelsText = ids.map(tableLabel).join(" and ");
      const summaryText = "Table " + tableLabelsText + " at " + hhmm + " on " + current.date
        + " (seats up to " + capacityOf(ids) + ")";

      const partyField = el("div", { class: "field" });
      partyField.appendChild(el("label", { for: "booking-party-size", text: "Party size" }));
      const partySizeInput = el("input", {
        type: "number", min: "1", step: "1", id: "booking-party-size",
        "data-testid": "booking-party-size", value: String(current.partySize),
      });
      partySizeInput.addEventListener("input", function () {
        attempt = null;
        clearSlot("booking-feedback");
      });
      partyField.appendChild(partySizeInput);

      const section = el("section", { "data-testid": "booking-form", class: "panel booking-form" }, [
        el("h2", { text: "Confirm your table" }),
        el("p", { "data-testid": "booking-summary", class: "booking-summary", text: summaryText }),
        partyField,
        el("button", {
          type: "button", "data-testid": "booking-submit", class: "btn btn-primary", text: "Book this table",
          onclick: function () { submitBooking(ids, slotObj); },
        }),
        el("div", { "data-slot": "booking-feedback" }),
      ]);
      bookingSlot.appendChild(section);
    }

    async function submitBooking(ids, slotObj) {
      const partySizeInput = q("booking-party-size");
      const partySize = parseInt(partySizeInput.value, 10);

      if (!attempt) {
        const body = { restaurant_id: current.restaurantId, starts_at_local: slotObj.starts_at_local, party_size: partySize };
        if (ids.length === 1) body.table_id = ids[0]; else body.table_ids = ids;
        attempt = { key: newIdempotencyKey(), body: body };
      }
      clearSlot("booking-feedback");
      try {
        const resp = await apiCall("/reservations", { method: "POST", json: attempt.body, idempotencyKey: attempt.key });
        if (resp.status === 201 || resp.status === 200) {
          renderConfirmation(resp.body);
          clearSlot("booking-feedback");
          return;
        }
        if (resp.status === 409 && resp.body && resp.body.error && resp.body.error.code === "table_unavailable") {
          showErrorIn("booking-feedback", "booking-error", errorMessage(resp, "That table was just taken."));
          refreshAvailability();
          return;
        }
        showErrorIn("booking-feedback", "booking-error", errorMessage(resp, "Booking failed."));
      } catch (_e) {
        showErrorIn("booking-feedback", "booking-uncertain",
          "We couldn't confirm whether your booking went through. Press “Book this table” again "
          + "to retry safely — it will not create a duplicate booking.");
      }
    }

    async function refreshAvailability() {
      if (!current) return;
      const mySeq = ++searchSeq;
      try {
        const resp = await apiCall("/availability", {
          auth: false,
          params: { restaurant_id: current.restaurantId, date: current.date, party_size: current.partySize },
        });
        if (mySeq !== searchSeq || !resp.ok) return;
        current = { restaurantId: current.restaurantId, date: current.date, partySize: current.partySize,
          restaurant: current.restaurant, slots: resp.body.slots };
        renderGrid();
      } catch (_e) { /* the booking-error already shown is enough feedback */ }
    }

    function renderConfirmation(reservation) {
      const confSlot = slot("confirmation");
      confSlot.innerHTML = "";
      const ids = reservation.table_ids || (reservation.table_id ? [reservation.table_id] : []);
      const tableLabelsText = ids.map(tableLabel).join(" and ");
      const parts = reservation.starts_at_local.split("T");
      const localDate = parts[0];
      const localTime = parts[1];
      const section = el("section", { "data-testid": "confirmation", class: "panel confirmation" }, [
        el("h2", { text: "You're booked" }),
        el("p", { class: "confirmation-reference-label", text: "Confirmation reference" }),
        el("p", { "data-testid": "confirmation-reference", class: "confirmation-reference", text: reservation.reference }),
        el("p", { "data-testid": "confirmation-details", class: "confirmation-details",
          text: current.restaurant.name + " — Table " + tableLabelsText + " at " + localTime + " on " + localDate }),
        el("p", { "data-testid": "confirmation-tables", class: "confirmation-tables", text: "Tables: " + tableLabelsText }),
      ]);
      confSlot.appendChild(section);
    }

    loadRestaurants();
  }

  // ===== /lookup =================================================================

  function initLookupPage() {
    const form = q("lookup-form");
    const input = q("lookup-reference-input");
    let lastRestaurant = null;

    function showReservationError(message) {
      clearSlot("reservation-detail");
      showErrorIn("reservation-error", "reservation-error", message);
    }

    function renderDetail(reservation, restaurant) {
      clearSlot("reservation-error");
      const s = slot("reservation-detail");
      s.innerHTML = "";
      const ids = reservation.table_ids || (reservation.table_id ? [reservation.table_id] : []);
      const byId = {};
      if (restaurant) for (const t of restaurant.tables) byId[t.id] = t.label;
      const tableLabelsText = ids.map(function (id) { return byId[id] || id; }).join(" and ");

      const section = el("section", { "data-testid": "reservation-detail", class: "reservation-detail" }, [
        el("p", { class: "reservation-restaurant", text: restaurant ? restaurant.name : reservation.restaurant_id }),
        el("p", { "data-testid": "reservation-status", class: "reservation-status", text: reservation.status }),
        el("p", { "data-testid": "reservation-tables", class: "reservation-tables", text: tableLabelsText }),
        el("p", { class: "reservation-time", text: reservation.starts_at_local + " (" + reservation.party_size + " guests)" }),
      ]);
      if (reservation.status === "confirmed") {
        section.appendChild(el("button", {
          type: "button", "data-testid": "reservation-cancel-button", class: "btn btn-danger", text: "Cancel reservation",
          onclick: function () { cancelReservation(reservation.reference); },
        }));
      }
      s.appendChild(section);
    }

    async function cancelReservation(reference) {
      try {
        const resp = await apiCall("/reservations/" + encodeURIComponent(reference) + "/cancel", { method: "POST" });
        if (resp.ok) {
          renderDetail(resp.body, lastRestaurant);
        } else {
          showErrorIn("reservation-error", "reservation-error", errorMessage(resp, "Could not cancel this reservation."));
        }
      } catch (_e) {
        showErrorIn("reservation-error", "reservation-error", "Could not reach the server. Please try again.");
      }
    }

    form.addEventListener("submit", async function (ev) {
      ev.preventDefault();
      clearSlot("reservation-error");
      clearSlot("reservation-detail");
      const reference = input.value.trim();
      try {
        const resp = await apiCall("/reservations/" + encodeURIComponent(reference));
        if (!resp.ok) {
          showReservationError(errorMessage(resp, "We couldn't find that reservation."));
          return;
        }
        const reservation = resp.body;
        lastRestaurant = null;
        try {
          const rResp = await apiCall("/restaurants/" + encodeURIComponent(reservation.restaurant_id), { auth: false });
          if (rResp.ok) lastRestaurant = rResp.body;
        } catch (_e) { /* labels fall back to raw ids */ }
        renderDetail(reservation, lastRestaurant);
      } catch (_e) {
        showReservationError("Could not reach the server. Please try again.");
      }
    });
  }
})();
