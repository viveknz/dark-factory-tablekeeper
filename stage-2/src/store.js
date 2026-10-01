"use strict";

const tz = require("./tz");
const { hashPassword, verifyPassword, newToken, newId, newReference } = require("./auth");
const { err } = require("./errors");

const WEEKDAYS = new Set(["mon", "tue", "wed", "thu", "fri", "sat", "sun"]);
const REFERENCE_RE = /^[A-Z0-9]{6,12}$/;

function isPlainObject(v) {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function requireId(value, label) {
  if (typeof value !== "string" || value.length < 1 || value.length > 64) {
    throw err(422, "validation_failed", `${label} must be a string of 1-64 characters`);
  }
  return value;
}

function requirePositiveInt(value, label) {
  if (typeof value !== "number" || !Number.isInteger(value) || value < 1) {
    throw err(422, "validation_failed", `${label} must be a positive integer`);
  }
  return value;
}

function pairKey(a, b) {
  return [a, b].slice().sort().join("");
}

/** Build the derived, queryable shape of one restaurant from its fixture-shaped fields. */
function buildRestaurant(raw) {
  if (!isPlainObject(raw)) throw err(422, "validation_failed", "invalid restaurant");
  const id = requireId(raw.id, "restaurant id");
  if (typeof raw.timezone !== "string" || !raw.timezone) {
    throw err(422, "validation_failed", "restaurant timezone is required");
  }
  const slot_minutes = requirePositiveInt(raw.slot_minutes, "slot_minutes");
  const reservation_duration_minutes = requirePositiveInt(
    raw.reservation_duration_minutes, "reservation_duration_minutes");
  const cancellation_cutoff_minutes = (() => {
    const v = raw.cancellation_cutoff_minutes;
    if (typeof v !== "number" || !Number.isInteger(v) || v < 0) {
      throw err(422, "validation_failed", "cancellation_cutoff_minutes must be a non-negative integer");
    }
    return v;
  })();
  const opening_hours = Array.isArray(raw.opening_hours) ? raw.opening_hours : [];
  const openingByWeekday = new Map();
  for (const entry of opening_hours) {
    if (!isPlainObject(entry) || !WEEKDAYS.has(entry.weekday)) {
      throw err(422, "validation_failed", "invalid opening_hours entry");
    }
    const opensMin = tz.minutesOfHHMM(entry.opens);
    const closesMin = tz.minutesOfHHMM(entry.closes);
    if (opensMin === null || closesMin === null || closesMin <= opensMin) {
      throw err(422, "validation_failed", "invalid opening_hours entry");
    }
    const list = openingByWeekday.get(entry.weekday) || [];
    list.push({ opensMin, closesMin });
    openingByWeekday.set(entry.weekday, list);
  }
  const tablesRaw = Array.isArray(raw.tables) ? raw.tables : [];
  const tables = [];
  const tablesById = new Map();
  for (const t of tablesRaw) {
    if (!isPlainObject(t)) throw err(422, "validation_failed", "invalid table");
    const tid = requireId(t.id, "table id");
    const capacity = requirePositiveInt(t.capacity, "table capacity");
    const label = typeof t.label === "string" ? t.label : String(t.label ?? "");
    const table = { id: tid, label, capacity };
    tables.push(table);
    tablesById.set(tid, table);
  }

  const combinableRaw = Array.isArray(raw.combinable) ? raw.combinable : [];
  const combinable = [];
  const combinableByKey = new Map();
  for (const entry of combinableRaw) {
    if (!Array.isArray(entry) || entry.length !== 2) {
      throw err(422, "validation_failed", "combinable entries must be a pair of table ids");
    }
    const [a, b] = entry;
    if (typeof a !== "string" || typeof b !== "string" || a === b) {
      throw err(422, "validation_failed", "combinable entries must be two distinct table ids");
    }
    if (!tablesById.has(a) || !tablesById.has(b)) {
      throw err(422, "validation_failed", "combinable entry references an unknown table");
    }
    const pair = [a, b];
    combinable.push(pair);
    combinableByKey.set(pairKey(a, b), pair);
  }

  return {
    id,
    name: typeof raw.name === "string" ? raw.name : "",
    timezone: raw.timezone,
    slot_minutes,
    reservation_duration_minutes,
    cancellation_cutoff_minutes,
    opening_hours,
    tables,
    tablesById,
    openingByWeekday,
    combinable,
    combinableByKey,
  };
}

function emptyWorldData() {
  return {
    users: new Map(),
    usersByEmail: new Map(),
    tokens: new Map(),
    restaurants: new Map(),
    reservations: new Map(),
    referencesIndex: new Map(),
    idempotency: new Map(),
  };
}

/** table_id or table_ids, normalized to a non-empty array of id strings. No validation of
 * count/duplicates/existence here -- that is shared with the live create/patch/move path. */
function rawTableIdsOf(raw) {
  if (Array.isArray(raw.table_ids)) return raw.table_ids;
  if (typeof raw.table_id === "string") return [raw.table_id];
  return null;
}

function buildReservation(raw, restaurants) {
  if (!isPlainObject(raw)) throw err(422, "validation_failed", "invalid reservation");
  const id = requireId(raw.id, "reservation id");
  const reference = raw.reference;
  if (typeof reference !== "string" || !REFERENCE_RE.test(reference)) {
    throw err(422, "validation_failed", "invalid reservation reference");
  }
  const restaurant = restaurants.get(raw.restaurant_id);
  if (!restaurant) throw err(422, "validation_failed", "unknown restaurant in seeded reservation");
  const tableIds = rawTableIdsOf(raw);
  if (!tableIds || tableIds.length < 1 || tableIds.length > 2) {
    throw err(422, "validation_failed", "seeded reservation needs table_id or table_ids");
  }
  for (const tid of tableIds) {
    if (!restaurant.tablesById.has(tid)) {
      throw err(422, "validation_failed", "unknown table in seeded reservation");
    }
  }
  let orderedIds = tableIds;
  if (tableIds.length === 2) {
    const declared = restaurant.combinableByKey.get(pairKey(tableIds[0], tableIds[1]));
    if (declared) orderedIds = declared;
  }
  const partySize = requirePositiveInt(raw.party_size, "party_size");
  const parsed = tz.parseLocal(raw.starts_at_local);
  if (!parsed) throw err(422, "validation_failed", "invalid starts_at_local in seeded reservation");
  const resolved = tz.wallToUTC(parsed.y, parsed.mo, parsed.d, parsed.h, parsed.mi, restaurant.timezone);
  if (!resolved.ok) throw err(422, "validation_failed", "seeded reservation local time does not exist");
  const startMs = resolved.utcMs;
  const endMs = startMs + restaurant.reservation_duration_minutes * 60000;
  const status = raw.status === "cancelled" ? "cancelled" : "confirmed";
  return {
    id,
    reference,
    user_id: typeof raw.user_id === "string" ? raw.user_id : "",
    restaurant_id: raw.restaurant_id,
    table_ids: orderedIds,
    party_size: partySize,
    status,
    starts_at_local: raw.starts_at_local,
    startMs,
    endMs,
    createdAtMs: Date.now(),
  };
}

function buildFromFixture(fixture) {
  if (!isPlainObject(fixture)) throw err(422, "validation_failed", "fixture must be an object");
  const data = emptyWorldData();

  const users = Array.isArray(fixture.users) ? fixture.users : [];
  for (const u of users) {
    if (!isPlainObject(u)) throw err(422, "validation_failed", "invalid user");
    const id = requireId(u.id, "user id");
    const email = typeof u.email === "string" ? u.email : "";
    const passwordHash = hashPassword(typeof u.password === "string" ? u.password : "");
    const display_name = typeof u.display_name === "string" ? u.display_name : "";
    data.users.set(id, { id, email, passwordHash, display_name });
    data.usersByEmail.set(email, id);
  }

  const restaurants = Array.isArray(fixture.restaurants) ? fixture.restaurants : [];
  for (const r of restaurants) {
    const built = buildRestaurant(r);
    data.restaurants.set(built.id, built);
  }

  const reservations = Array.isArray(fixture.reservations) ? fixture.reservations : [];
  for (const res of reservations) {
    const built = buildReservation(res, data.restaurants);
    data.reservations.set(built.id, built);
    data.referencesIndex.set(built.reference, built.id);
  }

  return data;
}

function canonicalize(value) {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (isPlainObject(value)) {
    const out = {};
    for (const k of Object.keys(value).sort()) out[k] = canonicalize(value[k]);
    return out;
  }
  return value;
}

function bodyKeyOf(body) {
  return JSON.stringify(canonicalize(body === undefined ? {} : body));
}

class World {
  constructor() {
    this.data = buildFromFixture({ users: [], restaurants: [], reservations: [] });
  }

  reset(fixture) {
    this.data = buildFromFixture(fixture);
  }

  exportState() {
    const d = this.data;
    return {
      track: "tablekeeper",
      format_version: 1,
      state: {
        users: [...d.users.values()],
        tokens: [...d.tokens.entries()],
        restaurants: [...d.restaurants.values()].map((r) => ({
          id: r.id,
          name: r.name,
          timezone: r.timezone,
          slot_minutes: r.slot_minutes,
          reservation_duration_minutes: r.reservation_duration_minutes,
          cancellation_cutoff_minutes: r.cancellation_cutoff_minutes,
          opening_hours: r.opening_hours,
          tables: r.tables,
          combinable: r.combinable,
        })),
        reservations: [...d.reservations.values()],
        idempotency: [...d.idempotency.entries()],
      },
    };
  }

  importState(payload) {
    if (!isPlainObject(payload)) throw err(422, "validation_failed", "import body must be an object");
    if (payload.track !== "tablekeeper" || payload.format_version !== 1 || !isPlainObject(payload.state)) {
      throw err(422, "validation_failed", "unrecognized export format");
    }
    const state = payload.state;
    const data = emptyWorldData();
    try {
      for (const u of Array.isArray(state.users) ? state.users : []) {
        if (!isPlainObject(u) || typeof u.id !== "string") throw new Error("bad user");
        data.users.set(u.id, {
          id: u.id, email: u.email, passwordHash: u.passwordHash, display_name: u.display_name,
        });
        data.usersByEmail.set(u.email, u.id);
      }
      for (const [token, userId] of Array.isArray(state.tokens) ? state.tokens : []) {
        data.tokens.set(token, userId);
      }
      for (const r of Array.isArray(state.restaurants) ? state.restaurants : []) {
        const built = buildRestaurant(r);
        data.restaurants.set(built.id, built);
      }
      // Accepts both this service's own export shape (table_ids) and a
      // preceding stage-1 export's shape (a single table_id), per the
      // cross-stage upgrade requirement.
      for (const res of Array.isArray(state.reservations) ? state.reservations : []) {
        if (!isPlainObject(res) || typeof res.id !== "string") throw new Error("bad reservation");
        const table_ids = Array.isArray(res.table_ids) ? res.table_ids
          : typeof res.table_id === "string" ? [res.table_id] : null;
        if (!table_ids) throw new Error("bad reservation tables");
        data.reservations.set(res.id, { ...res, table_ids });
        if (typeof res.reference === "string") data.referencesIndex.set(res.reference, res.id);
      }
      for (const [key, record] of Array.isArray(state.idempotency) ? state.idempotency : []) {
        data.idempotency.set(key, record);
      }
    } catch (_e) {
      throw err(422, "validation_failed", "malformed export state");
    }
    this.data = data;
  }

  // ---- auth --------------------------------------------------------------

  signup({ email, password, display_name }) {
    if (typeof email !== "string" || typeof password !== "string" || typeof display_name !== "string") {
      throw err(400, "malformed_request", "email, password and display_name must be strings");
    }
    const parts = email.split("@");
    if (parts.length !== 2 || parts[0].length === 0 || parts[1].length === 0 || /\s/.test(email)) {
      throw err(422, "validation_failed", "email must be of the form local@domain");
    }
    if (password.length < 8) {
      throw err(422, "validation_failed", "password must be at least 8 characters");
    }
    if (this.data.usersByEmail.has(email)) {
      throw err(409, "email_taken", "email already registered");
    }
    const id = newId("u");
    const passwordHash = hashPassword(password);
    this.data.users.set(id, { id, email, passwordHash, display_name });
    this.data.usersByEmail.set(email, id);
    const token = newToken();
    this.data.tokens.set(token, id);
    return { user_id: id, display_name, token };
  }

  login({ email, password }) {
    if (typeof email !== "string" || typeof password !== "string") {
      throw err(400, "malformed_request", "email and password must be strings");
    }
    const userId = this.data.usersByEmail.get(email);
    const user = userId ? this.data.users.get(userId) : null;
    if (!user || !verifyPassword(password, user.passwordHash)) {
      throw err(401, "unauthenticated", "wrong email or password");
    }
    const token = newToken();
    this.data.tokens.set(token, user.id);
    return { user_id: user.id, display_name: user.display_name, token };
  }

  authenticate(token) {
    if (!token) return null;
    const userId = this.data.tokens.get(token);
    return userId || null;
  }

  // ---- restaurants / availability ----------------------------------------

  listRestaurants() {
    return [...this.data.restaurants.values()].map((r) => ({
      id: r.id, name: r.name, timezone: r.timezone,
    }));
  }

  getRestaurantDetail(id) {
    const r = this.data.restaurants.get(id);
    if (!r) throw err(404, "not_found", "unknown restaurant");
    return {
      id: r.id,
      name: r.name,
      timezone: r.timezone,
      slot_minutes: r.slot_minutes,
      reservation_duration_minutes: r.reservation_duration_minutes,
      cancellation_cutoff_minutes: r.cancellation_cutoff_minutes,
      opening_hours: r.opening_hours,
      tables: r.tables,
      combinable: r.combinable,
    };
  }

  /** Whether `tableId` has a confirmed, overlapping reservation (single or combined). */
  hasOverlap(restaurantId, tableId, startMs, endMs, excludeId) {
    for (const r of this.data.reservations.values()) {
      if (r.status !== "confirmed") continue;
      if (r.restaurant_id !== restaurantId) continue;
      if (excludeId && r.id === excludeId) continue;
      if (!r.table_ids.includes(tableId)) continue;
      if (startMs < r.endMs && r.startMs < endMs) return true;
    }
    return false;
  }

  hasOverlapForSet(restaurantId, tableIds, startMs, endMs, excludeId) {
    return tableIds.some((id) => this.hasOverlap(restaurantId, id, startMs, endMs, excludeId));
  }

  availability({ restaurant_id, date, party_size }) {
    const restaurant = this.data.restaurants.get(restaurant_id);
    if (!restaurant) throw err(404, "not_found", "unknown restaurant");
    const d = tz.parseDate(date);
    if (!d) throw err(422, "validation_failed", "date must be a valid YYYY-MM-DD");
    const weekday = tz.weekdayOf(d.y, d.mo, d.d);
    const entries = restaurant.openingByWeekday.get(weekday) || [];
    const slots = [];
    for (const entry of entries) {
      for (
        let t = entry.opensMin;
        t + restaurant.reservation_duration_minutes <= entry.closesMin;
        t += restaurant.slot_minutes
      ) {
        const hhmm = tz.hhmmOfMinutes(t);
        const resolved = tz.wallToUTC(d.y, d.mo, d.d, Math.floor(t / 60), t % 60, restaurant.timezone);
        if (!resolved.ok) continue; // spring-forward gap: never appears
        const startMs = resolved.utcMs;
        const endMs = startMs + restaurant.reservation_duration_minutes * 60000;

        const freeSingle = (table) => !this.hasOverlap(restaurant.id, table.id, startMs, endMs, null);

        const available_table_ids = restaurant.tables
          .filter((table) => table.capacity >= party_size)
          .filter(freeSingle)
          .map((table) => table.id);

        const available_options = [];
        for (const table of restaurant.tables) {
          if (table.capacity >= party_size && freeSingle(table)) {
            available_options.push({ table_ids: [table.id], capacity: table.capacity });
          }
        }
        for (const [a, b] of restaurant.combinable) {
          const tableA = restaurant.tablesById.get(a);
          const tableB = restaurant.tablesById.get(b);
          const capacity = tableA.capacity + tableB.capacity;
          if (capacity >= party_size && freeSingle(tableA) && freeSingle(tableB)) {
            available_options.push({ table_ids: [a, b], capacity });
          }
        }

        slots.push({
          starts_at_local: `${date}T${hhmm}`,
          starts_at: tz.formatInZone(startMs, restaurant.timezone),
          available_table_ids,
          available_options,
          _sort: startMs,
        });
      }
    }
    slots.sort((a, b) => a._sort - b._sort);
    for (const s of slots) delete s._sort;
    return {
      restaurant_id: restaurant.id,
      date,
      timezone: restaurant.timezone,
      slots,
    };
  }

  // ---- reservations --------------------------------------------------------

  reservationView(r) {
    const restaurant = this.data.restaurants.get(r.restaurant_id);
    const view = {
      reservation_id: r.id,
      reference: r.reference,
      restaurant_id: r.restaurant_id,
      table_ids: r.table_ids,
      party_size: r.party_size,
      status: r.status,
      starts_at_local: r.starts_at_local,
      starts_at: tz.formatInZone(r.startMs, restaurant.timezone),
      ends_at: tz.formatInZone(r.endMs, restaurant.timezone),
      created_at: tz.formatUTC(r.createdAtMs),
    };
    if (r.table_ids.length === 1) view.table_id = r.table_ids[0];
    return view;
  }

  /** table_id or table_ids (not both) resolved to a validated, ordered id array. */
  resolveTableIds(restaurant, body, current) {
    const hasSingle = Object.prototype.hasOwnProperty.call(body, "table_id");
    const hasMulti = Object.prototype.hasOwnProperty.call(body, "table_ids");
    if (hasSingle && hasMulti) {
      throw err(422, "validation_failed", "table_id and table_ids are mutually exclusive");
    }
    let ids;
    if (hasMulti) {
      const v = body.table_ids;
      if (!Array.isArray(v) || v.some((x) => typeof x !== "string")) {
        throw err(400, "malformed_request", "table_ids must be an array of strings");
      }
      ids = v;
    } else if (hasSingle) {
      if (typeof body.table_id !== "string") {
        throw err(400, "malformed_request", "table_id must be a string");
      }
      ids = [body.table_id];
    } else if (current) {
      ids = current.table_ids.slice();
    } else {
      throw err(422, "validation_failed", "table_id or table_ids is required");
    }
    if (ids.length < 1) throw err(422, "validation_failed", "at least one table is required");
    if (new Set(ids).size !== ids.length) {
      throw err(422, "validation_failed", "duplicate table id in the set");
    }
    if (ids.length > 2) throw err(422, "combination_not_allowed", "more than two tables");
    return ids;
  }

  /**
   * Validate the combined result of `body` applied over `current` (or nothing,
   * for a fresh create) and resolve it to a concrete table set/party/time.
   * Shared by create, patch and each item of a reservation-move.
   */
  prepareChange(restaurant, body, current) {
    const requestedIds = this.resolveTableIds(restaurant, body, current);
    const tables = requestedIds.map((tid) => {
      const table = restaurant.tablesById.get(tid);
      if (!table) throw err(404, "not_found", "unknown table");
      return table;
    });
    let tableIds = requestedIds;
    if (tables.length === 2) {
      const declared = restaurant.combinableByKey.get(pairKey(requestedIds[0], requestedIds[1]));
      if (!declared) throw err(422, "combination_not_allowed", "that pair of tables is not combinable");
      tableIds = declared;
    }
    const capacity = tables.reduce((sum, t) => sum + t.capacity, 0);

    let partySize;
    if (Object.prototype.hasOwnProperty.call(body, "party_size")) {
      const v = body.party_size;
      if (typeof v !== "number" || !Number.isInteger(v) || v < 1) {
        throw err(422, "validation_failed", "party_size must be a positive integer");
      }
      partySize = v;
    } else if (current) {
      partySize = current.party_size;
    } else {
      throw err(422, "validation_failed", "party_size is required");
    }
    if (partySize > capacity) {
      throw err(422, "party_exceeds_capacity", "party size exceeds table capacity");
    }

    let startsAtLocalStr;
    if (Object.prototype.hasOwnProperty.call(body, "starts_at_local")) {
      startsAtLocalStr = body.starts_at_local;
    } else if (current) {
      startsAtLocalStr = current.starts_at_local;
    } else {
      throw err(422, "validation_failed", "starts_at_local is required");
    }
    const parsed = tz.parseLocal(startsAtLocalStr);
    if (!parsed) {
      throw err(422, "validation_failed", "starts_at_local must be a bare local YYYY-MM-DDTHH:MM");
    }

    const startMinutes = parsed.h * 60 + parsed.mi;
    const duration = restaurant.reservation_duration_minutes;
    const weekday = tz.weekdayOf(parsed.y, parsed.mo, parsed.d);
    const entries = restaurant.openingByWeekday.get(weekday) || [];
    let matched = null;
    for (const e of entries) {
      if (startMinutes >= e.opensMin && startMinutes + duration <= e.closesMin) { matched = e; break; }
    }
    if (!matched) throw err(422, "outside_opening_hours", "outside opening hours");
    if ((startMinutes - matched.opensMin) % restaurant.slot_minutes !== 0) {
      throw err(422, "not_on_slot_grid", "starts_at_local is not on the slot grid");
    }
    const resolved = tz.wallToUTC(parsed.y, parsed.mo, parsed.d, parsed.h, parsed.mi, restaurant.timezone);
    if (!resolved.ok) throw err(422, "invalid_local_time", "local time does not exist");
    const startMs = resolved.utcMs;
    const endMs = startMs + duration * 60000;
    return { tableIds, partySize, startsAtLocalStr, startMs, endMs };
  }

  createReservation(userId, body) {
    if (!isPlainObject(body)) throw err(400, "malformed_request", "body must be a JSON object");
    if (typeof body.restaurant_id !== "string") {
      if (body.restaurant_id === undefined) throw err(422, "validation_failed", "restaurant_id is required");
      throw err(400, "malformed_request", "restaurant_id must be a string");
    }
    const restaurant = this.data.restaurants.get(body.restaurant_id);
    if (!restaurant) throw err(404, "not_found", "unknown restaurant");

    const prepared = this.prepareChange(restaurant, body, null);
    if (this.hasOverlapForSet(restaurant.id, prepared.tableIds, prepared.startMs, prepared.endMs, null)) {
      throw err(409, "table_unavailable", "table is not available for that interval");
    }

    const id = newId("res");
    const reference = newReference(this.data.referencesIndex);
    const reservation = {
      id,
      reference,
      user_id: userId,
      restaurant_id: restaurant.id,
      table_ids: prepared.tableIds,
      party_size: prepared.partySize,
      status: "confirmed",
      starts_at_local: prepared.startsAtLocalStr,
      startMs: prepared.startMs,
      endMs: prepared.endMs,
      createdAtMs: Date.now(),
    };
    this.data.reservations.set(id, reservation);
    this.data.referencesIndex.set(reference, id);
    return this.reservationView(reservation);
  }

  findOwned(userId, reference) {
    const id = this.data.referencesIndex.get(reference);
    const r = id ? this.data.reservations.get(id) : null;
    if (!r || r.user_id !== userId) throw err(404, "not_found", "unknown reservation");
    return r;
  }

  getReservation(userId, reference) {
    return this.reservationView(this.findOwned(userId, reference));
  }

  listReservations(userId) {
    const mine = [...this.data.reservations.values()].filter((r) => r.user_id === userId);
    mine.sort((a, b) => b.startMs - a.startMs);
    return mine.map((r) => this.reservationView(r));
  }

  checkCutoff(restaurant, reservation) {
    const cutoffMs = restaurant.cancellation_cutoff_minutes * 60000;
    if (Date.now() >= reservation.startMs - cutoffMs) {
      throw err(409, "cutoff_passed", "the cancellation/amendment cutoff has passed");
    }
  }

  cancelReservation(userId, reference) {
    const r = this.findOwned(userId, reference);
    if (r.status === "cancelled") return this.reservationView(r);
    const restaurant = this.data.restaurants.get(r.restaurant_id);
    this.checkCutoff(restaurant, r);
    r.status = "cancelled";
    return this.reservationView(r);
  }

  patchReservation(userId, reference, body) {
    if (!isPlainObject(body)) throw err(400, "malformed_request", "body must be a JSON object");
    const r = this.findOwned(userId, reference);
    if (r.status === "cancelled") throw err(409, "reservation_cancelled", "reservation is cancelled");
    const restaurant = this.data.restaurants.get(r.restaurant_id);
    this.checkCutoff(restaurant, r);
    const prepared = this.prepareChange(restaurant, body, r);
    if (this.hasOverlapForSet(restaurant.id, prepared.tableIds, prepared.startMs, prepared.endMs, r.id)) {
      throw err(409, "table_unavailable", "table is not available for that interval");
    }
    r.table_ids = prepared.tableIds;
    r.party_size = prepared.partySize;
    r.starts_at_local = prepared.startsAtLocalStr;
    r.startMs = prepared.startMs;
    r.endMs = prepared.endMs;
    return this.reservationView(r);
  }

  createMoves(userId, body) {
    if (!isPlainObject(body) || !Array.isArray(body.moves)) {
      throw err(422, "validation_failed", "moves must be an array");
    }
    const moves = body.moves;
    if (moves.length < 1 || moves.length > 8) {
      throw err(422, "validation_failed", "moves must contain 1 to 8 items");
    }
    const seenRefs = new Set();
    for (const m of moves) {
      if (!isPlainObject(m) || typeof m.reference !== "string") {
        throw err(422, "validation_failed", "each move needs a string reference");
      }
      if (seenRefs.has(m.reference)) {
        throw err(422, "validation_failed", "duplicate reference in moves");
      }
      seenRefs.add(m.reference);
    }

    const records = moves.map((m) => ({ move: m, reservation: this.findOwned(userId, m.reference) }));
    const restaurantId = records[0].reservation.restaurant_id;
    for (const rec of records) {
      if (rec.reservation.restaurant_id !== restaurantId) {
        throw err(422, "validation_failed", "all bookings in a move must belong to the same restaurant");
      }
    }
    const restaurant = this.data.restaurants.get(restaurantId);

    const prepared = [];
    for (const rec of records) {
      const r = rec.reservation;
      if (r.status === "cancelled") throw err(409, "reservation_cancelled", "reservation is cancelled");
      this.checkCutoff(restaurant, r);
      const change = this.prepareChange(restaurant, rec.move, r);
      prepared.push({ r, change });
    }

    // Batch occupancy check: the resulting set against itself and against
    // every other confirmed reservation not part of this batch.
    const movedIds = new Set(records.map((rec) => rec.reservation.id));
    const timeOverlap = (a, b) => a.change.startMs < b.change.endMs && b.change.startMs < a.change.endMs;
    for (let i = 0; i < prepared.length; i++) {
      const a = prepared[i];
      for (let j = 0; j < prepared.length; j++) {
        if (i === j) continue;
        const b = prepared[j];
        const sharesTable = a.change.tableIds.some((id) => b.change.tableIds.includes(id));
        if (sharesTable && timeOverlap(a, b)) {
          throw err(409, "table_unavailable", "resulting bookings overlap each other");
        }
      }
      for (const other of this.data.reservations.values()) {
        if (other.status !== "confirmed" || movedIds.has(other.id)) continue;
        if (other.restaurant_id !== restaurantId) continue;
        const sharesTable = a.change.tableIds.some((id) => other.table_ids.includes(id));
        if (!sharesTable) continue;
        if (a.change.startMs < other.endMs && other.startMs < a.change.endMs) {
          throw err(409, "table_unavailable", "table is not available for that interval");
        }
      }
    }

    for (const { r, change } of prepared) {
      r.table_ids = change.tableIds;
      r.party_size = change.partySize;
      r.starts_at_local = change.startsAtLocalStr;
      r.startMs = change.startMs;
      r.endMs = change.endMs;
    }
    return prepared.map(({ r }) => this.reservationView(r));
  }

  // ---- idempotency ---------------------------------------------------------

  idempotencyKeyOf(userId, key, method, path) {
    return `${userId} ${key} ${method} ${path}`;
  }

  resolveIdempotency(userId, key, method, path, body) {
    const compositeKey = this.idempotencyKeyOf(userId, key, method, path);
    const existing = this.data.idempotency.get(compositeKey);
    const requestBodyKey = bodyKeyOf(body);
    if (existing) {
      if (existing.bodyKey === requestBodyKey) {
        return { replay: true, body: existing.body };
      }
      throw err(409, "idempotency_key_reuse", "idempotency key reused with a different body");
    }
    return { replay: false, compositeKey, requestBodyKey };
  }

  storeIdempotency(compositeKey, requestBodyKey, responseBody) {
    this.data.idempotency.set(compositeKey, { bodyKey: requestBodyKey, body: responseBody });
  }
}

module.exports = { World };
