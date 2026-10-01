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
  return [a, b].slice().sort().join("");
}

function plainObjectFromMap(map) {
  const out = {};
  for (const [k, v] of map) out[k] = v;
  return out;
}

function sameOrderedIds(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

/** Build { opening_hours, openingByWeekday } from a raw fixture/policy array.
 * `strictNoDuplicateWeekday` rejects more than one entry per weekday (policies);
 * the restaurant fixture itself stays lenient, matching stages 1-2. */
function buildOpeningHours(rawArray, strictNoDuplicateWeekday) {
  const opening_hours = Array.isArray(rawArray) ? rawArray : [];
  const openingByWeekday = new Map();
  const seenWeekdays = new Set();
  for (const entry of opening_hours) {
    if (!isPlainObject(entry) || !WEEKDAYS.has(entry.weekday)) {
      throw err(422, "validation_failed", "invalid opening_hours entry");
    }
    if (strictNoDuplicateWeekday) {
      if (seenWeekdays.has(entry.weekday)) {
        throw err(422, "validation_failed", "duplicate weekday in opening_hours");
      }
      seenWeekdays.add(entry.weekday);
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
  return { opening_hours, openingByWeekday };
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
  const { opening_hours, openingByWeekday } = buildOpeningHours(raw.opening_hours, false);

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

  const manager_user_ids = new Set(
    Array.isArray(raw.manager_user_ids) ? raw.manager_user_ids.filter((x) => typeof x === "string") : []
  );

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
    manager_user_ids,
    policies: [],       // published policies, in publication order; policy_version >= 1
    nextPolicyVersion: 1,
  };
}

/** The restaurant's own fixture configuration, as "policy 0". */
function policyZeroOf(restaurant) {
  const capacities = new Map(restaurant.tables.map((t) => [t.id, t.capacity]));
  return {
    policy_version: 0,
    effective_from: null,
    ef_ms: -Infinity,
    slot_minutes: restaurant.slot_minutes,
    reservation_duration_minutes: restaurant.reservation_duration_minutes,
    cancellation_cutoff_minutes: restaurant.cancellation_cutoff_minutes,
    opening_hours: restaurant.opening_hours,
    openingByWeekday: restaurant.openingByWeekday,
    capacities,
  };
}

/** The policy in force for a given local calendar date: greatest effective_from not
 * later than the date, ties broken by greatest policy_version. Policy 0 always qualifies. */
function resolvePolicy(restaurant, y, mo, d) {
  const targetMs = Date.UTC(y, mo - 1, d);
  let best = policyZeroOf(restaurant);
  for (const p of restaurant.policies) {
    if (p.ef_ms > targetMs) continue;
    if (p.ef_ms > best.ef_ms || (p.ef_ms === best.ef_ms && p.policy_version > best.policy_version)) {
      best = p;
    }
  }
  return best;
}

function acceptedTermsOf(policy) {
  return {
    policy_version: policy.policy_version,
    slot_minutes: policy.slot_minutes,
    reservation_duration_minutes: policy.reservation_duration_minutes,
    cancellation_cutoff_minutes: policy.cancellation_cutoff_minutes,
    opening_hours: policy.opening_hours,
    capacities: plainObjectFromMap(policy.capacities),
  };
}

function policyView(p) {
  return {
    effective_from: p.effective_from,
    policy_version: p.policy_version,
    slot_minutes: p.slot_minutes,
    reservation_duration_minutes: p.reservation_duration_minutes,
    cancellation_cutoff_minutes: p.cancellation_cutoff_minutes,
    opening_hours: p.opening_hours,
    capacities: plainObjectFromMap(p.capacities),
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
    series: new Map(),
  };
}

function rawTableIdsOf(raw) {
  if (Array.isArray(raw.table_ids)) return raw.table_ids;
  if (typeof raw.table_id === "string") return [raw.table_id];
  return null;
}

/** A single synthesized "created" history entry, used for seeded/imported
 * reservations that did not carry their own history. */
function createdChangesOf(reservation) {
  const changes = [];
  if (reservation.table_ids.length === 2) {
    changes.push({ field: "table_ids", from: null, to: reservation.table_ids.slice() });
  } else {
    changes.push({ field: "table_id", from: null, to: reservation.table_ids[0] });
  }
  changes.push({ field: "starts_at_local", from: null, to: reservation.starts_at_local });
  changes.push({ field: "party_size", from: null, to: reservation.party_size });
  return changes;
}

function recordHistory(reservation, event, changes, atMs) {
  reservation.history.push({
    seq: reservation.history.length + 1,
    atMs: atMs === undefined ? Date.now() : atMs,
    event,
    changes,
    revision: reservation.revision,
    accepted_terms: reservation.accepted_terms,
  });
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
  const createdAtMs = Date.now();
  const reservation = {
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
    createdAtMs,
    revision: 1,
    accepted_terms: acceptedTermsOf(policyZeroOf(restaurant)),
    history: [],
    seriesId: null,
    seriesIndex: null,
    exception: false,
  };
  recordHistory(reservation, "created", createdChangesOf(reservation), createdAtMs);
  return reservation;
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

function addDaysLocal(y, mo, d, days) {
  const ms = Date.UTC(y, mo - 1, d) + days * 86400000;
  const dt = new Date(ms);
  return { y: dt.getUTCFullYear(), mo: dt.getUTCMonth() + 1, d: dt.getUTCDate() };
}

function pad2(n) {
  return String(n).padStart(2, "0");
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
          manager_user_ids: [...r.manager_user_ids],
          policies: r.policies.map((p) => ({
            policy_version: p.policy_version,
            effective_from: p.effective_from,
            slot_minutes: p.slot_minutes,
            reservation_duration_minutes: p.reservation_duration_minutes,
            cancellation_cutoff_minutes: p.cancellation_cutoff_minutes,
            opening_hours: p.opening_hours,
            capacities: plainObjectFromMap(p.capacities),
          })),
          nextPolicyVersion: r.nextPolicyVersion,
        })),
        reservations: [...d.reservations.values()].map((r) => ({
          ...r,
          history: r.history,
        })),
        idempotency: [...d.idempotency.entries()],
        series: [...d.series.entries()],
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
      // Accepts this service's own export shape, a stage-2 export (combinable,
      // table_ids, no policies) and a stage-1 export (single table_id, no
      // combinable, no policies) alike.
      for (const r of Array.isArray(state.restaurants) ? state.restaurants : []) {
        const built = buildRestaurant(r);
        if (Array.isArray(r.policies)) {
          for (const p of r.policies) {
            const { opening_hours, openingByWeekday } = buildOpeningHours(p.opening_hours, true);
            const efParsed = tz.parseDate(p.effective_from);
            if (!efParsed) throw new Error("bad policy effective_from");
            const capacities = new Map(Object.entries(isPlainObject(p.capacities) ? p.capacities : {}));
            built.policies.push({
              policy_version: p.policy_version,
              effective_from: p.effective_from,
              ef_ms: Date.UTC(efParsed.y, efParsed.mo - 1, efParsed.d),
              slot_minutes: p.slot_minutes,
              reservation_duration_minutes: p.reservation_duration_minutes,
              cancellation_cutoff_minutes: p.cancellation_cutoff_minutes,
              opening_hours,
              openingByWeekday,
              capacities,
            });
          }
          built.nextPolicyVersion = typeof r.nextPolicyVersion === "number"
            ? r.nextPolicyVersion
            : built.policies.reduce((m, p) => Math.max(m, p.policy_version), 0) + 1;
        }
        data.restaurants.set(built.id, built);
      }
      for (const res of Array.isArray(state.reservations) ? state.reservations : []) {
        if (!isPlainObject(res) || typeof res.id !== "string") throw new Error("bad reservation");
        const table_ids = Array.isArray(res.table_ids) ? res.table_ids
          : typeof res.table_id === "string" ? [res.table_id] : null;
        if (!table_ids) throw new Error("bad reservation tables");
        const restaurant = data.restaurants.get(res.restaurant_id);
        const hasOwnTerms = res.accepted_terms && typeof res.revision === "number";
        const accepted_terms = hasOwnTerms ? res.accepted_terms
          : acceptedTermsOf(policyZeroOf(restaurant));
        const revision = hasOwnTerms ? res.revision : 1;
        const history = Array.isArray(res.history) && res.history.length
          ? res.history
          : null;
        const reservation = {
          ...res,
          table_ids,
          revision,
          accepted_terms,
          seriesId: typeof res.seriesId === "string" ? res.seriesId : null,
          seriesIndex: typeof res.seriesIndex === "number" ? res.seriesIndex : null,
          exception: !!res.exception,
          history: history || [],
        };
        if (!history) {
          recordHistory(reservation, "created", createdChangesOf(reservation), res.createdAtMs);
        }
        data.reservations.set(res.id, reservation);
        if (typeof res.reference === "string") data.referencesIndex.set(res.reference, res.id);
      }
      for (const [key, record] of Array.isArray(state.idempotency) ? state.idempotency : []) {
        data.idempotency.set(key, record);
      }
      for (const [sid, s] of Array.isArray(state.series) ? state.series : []) {
        data.series.set(sid, s);
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

  // ---- restaurants / policies / availability -------------------------------

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

  listPolicies(restaurantId) {
    const r = this.data.restaurants.get(restaurantId);
    if (!r) throw err(404, "not_found", "unknown restaurant");
    return { policies: r.policies.map(policyView) };
  }

  publishPolicy(userId, restaurantId, body) {
    const r = this.data.restaurants.get(restaurantId);
    if (!r) throw err(404, "not_found", "unknown restaurant");
    if (!r.manager_user_ids.has(userId)) throw err(403, "forbidden", "not a manager of this restaurant");
    if (!isPlainObject(body)) throw err(422, "validation_failed", "policy body must be an object");

    if (typeof body.effective_from !== "string") {
      throw err(422, "validation_failed", "effective_from is required");
    }
    const efParsed = tz.parseDate(body.effective_from);
    if (!efParsed) throw err(422, "validation_failed", "effective_from must be a valid YYYY-MM-DD date");

    const intInRange = (v, lo, hi) => typeof v === "number" && Number.isInteger(v) && v >= lo && v <= hi;
    if (!intInRange(body.slot_minutes, 1, 1440)) {
      throw err(422, "validation_failed", "slot_minutes must be an integer 1..1440");
    }
    if (!intInRange(body.reservation_duration_minutes, 1, 1440)) {
      throw err(422, "validation_failed", "reservation_duration_minutes must be an integer 1..1440");
    }
    if (!intInRange(body.cancellation_cutoff_minutes, 0, 10080)) {
      throw err(422, "validation_failed", "cancellation_cutoff_minutes must be an integer 0..10080");
    }
    const { opening_hours, openingByWeekday } = buildOpeningHours(body.opening_hours, true);

    if (!isPlainObject(body.capacities)) {
      throw err(422, "validation_failed", "capacities must be an object");
    }
    const givenIds = Object.keys(body.capacities);
    const restaurantIds = [...r.tablesById.keys()];
    if (givenIds.length !== restaurantIds.length || !restaurantIds.every((id) => Object.prototype.hasOwnProperty.call(body.capacities, id))) {
      throw err(422, "validation_failed", "capacities must name exactly the restaurant's table ids");
    }
    const capacities = new Map();
    for (const id of restaurantIds) {
      const v = body.capacities[id];
      if (!intInRange(v, 1, 100)) {
        throw err(422, "validation_failed", "capacities values must be integers 1..100");
      }
      capacities.set(id, v);
    }

    const policy_version = r.nextPolicyVersion++;
    const policy = {
      policy_version,
      effective_from: body.effective_from,
      ef_ms: Date.UTC(efParsed.y, efParsed.mo - 1, efParsed.d),
      slot_minutes: body.slot_minutes,
      reservation_duration_minutes: body.reservation_duration_minutes,
      cancellation_cutoff_minutes: body.cancellation_cutoff_minutes,
      opening_hours,
      openingByWeekday,
      capacities,
    };
    r.policies.push(policy);
    return policyView(policy);
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

  availability({ restaurant_id, date, party_size, explain }) {
    const restaurant = this.data.restaurants.get(restaurant_id);
    if (!restaurant) throw err(404, "not_found", "unknown restaurant");
    const d = tz.parseDate(date);
    if (!d) throw err(422, "validation_failed", "date must be a valid YYYY-MM-DD");
    const policy = resolvePolicy(restaurant, d.y, d.mo, d.d);
    const weekday = tz.weekdayOf(d.y, d.mo, d.d);
    const entries = policy.openingByWeekday.get(weekday) || [];
    const slots = [];
    for (const entry of entries) {
      for (
        let t = entry.opensMin;
        t + policy.reservation_duration_minutes <= entry.closesMin;
        t += policy.slot_minutes
      ) {
        const hhmm = tz.hhmmOfMinutes(t);
        const resolved = tz.wallToUTC(d.y, d.mo, d.d, Math.floor(t / 60), t % 60, restaurant.timezone);
        if (!resolved.ok) continue; // spring-forward gap: never appears
        const startMs = resolved.utcMs;
        const endMs = startMs + policy.reservation_duration_minutes * 60000;

        const freeSingle = (table) => !this.hasOverlap(restaurant.id, table.id, startMs, endMs, null);
        const capOf = (tableId) => policy.capacities.get(tableId);

        const available_table_ids = restaurant.tables
          .filter((table) => capOf(table.id) >= party_size)
          .filter(freeSingle)
          .map((table) => table.id);

        const available_options = [];
        for (const table of restaurant.tables) {
          if (capOf(table.id) >= party_size && freeSingle(table)) {
            available_options.push({ table_ids: [table.id], capacity: capOf(table.id) });
          }
        }
        for (const [a, b] of restaurant.combinable) {
          const tableA = restaurant.tablesById.get(a);
          const tableB = restaurant.tablesById.get(b);
          const capacity = capOf(a) + capOf(b);
          if (capacity >= party_size && freeSingle(tableA) && freeSingle(tableB)) {
            available_options.push({ table_ids: [a, b], capacity });
          }
        }

        const slot = {
          starts_at_local: `${date}T${hhmm}`,
          starts_at: tz.formatInZone(startMs, restaurant.timezone),
          available_table_ids,
          available_options,
          _sort: startMs,
        };
        if (explain) {
          slot.explain = restaurant.tables.map((table) => {
            const capHolds = capOf(table.id) >= party_size;
            const overlapHolds = !this.hasOverlap(restaurant.id, table.id, startMs, endMs, null);
            return {
              table_id: table.id,
              policy_version: policy.policy_version,
              available: capHolds && overlapHolds,
              rules: [
                { rule: "capacity", holds: capHolds },
                { rule: "no_overlap", holds: overlapHolds },
              ],
            };
          });
        }
        slots.push(slot);
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
      revision: r.revision,
      accepted_terms: r.accepted_terms,
    };
    if (r.table_ids.length === 1) view.table_id = r.table_ids[0];
    return view;
  }

  /** Structural resolution of the requested table/party/time fields: XOR, duplicate,
   * count, existence and combinable-pair checks, plus basic type/format validation.
   * Deliberately policy-independent, so a genuine no-op PATCH can be detected (and
   * short-circuited) without being exposed to a policy that changed after the
   * booking was made -- "a policy publication does not change existing bookings". */
  resolveRequestedFields(restaurant, body, current) {
    const hasSingle = Object.prototype.hasOwnProperty.call(body, "table_id");
    const hasMulti = Object.prototype.hasOwnProperty.call(body, "table_ids");
    if (hasSingle && hasMulti) {
      throw err(422, "validation_failed", "table_id and table_ids are mutually exclusive");
    }
    let requestedIds;
    if (hasMulti) {
      const v = body.table_ids;
      if (!Array.isArray(v) || v.some((x) => typeof x !== "string")) {
        throw err(400, "malformed_request", "table_ids must be an array of strings");
      }
      requestedIds = v;
    } else if (hasSingle) {
      if (typeof body.table_id !== "string") {
        throw err(400, "malformed_request", "table_id must be a string");
      }
      requestedIds = [body.table_id];
    } else if (current) {
      requestedIds = current.table_ids.slice();
    } else {
      throw err(422, "validation_failed", "table_id or table_ids is required");
    }
    if (requestedIds.length < 1) throw err(422, "validation_failed", "at least one table is required");
    if (new Set(requestedIds).size !== requestedIds.length) {
      throw err(422, "validation_failed", "duplicate table id in the set");
    }
    if (requestedIds.length > 2) throw err(422, "combination_not_allowed", "more than two tables");

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

    return { tableIds, partySize, startsAtLocalStr, parsed };
  }

  /** Policy-dependent validation of an already-structurally-resolved change:
   * opening hours, slot grid, DST and capacity, against the policy in force for
   * the requested local date. Shared by create, a real (non-no-op) patch, each
   * move item and each generated series occurrence. */
  applyPolicyValidation(restaurant, basic) {
    const { tableIds, partySize, startsAtLocalStr, parsed } = basic;
    const policy = resolvePolicy(restaurant, parsed.y, parsed.mo, parsed.d);

    const startMinutes = parsed.h * 60 + parsed.mi;
    const duration = policy.reservation_duration_minutes;
    const weekday = tz.weekdayOf(parsed.y, parsed.mo, parsed.d);
    const entries = policy.openingByWeekday.get(weekday) || [];
    let matched = null;
    for (const e of entries) {
      if (startMinutes >= e.opensMin && startMinutes + duration <= e.closesMin) { matched = e; break; }
    }
    if (!matched) throw err(422, "outside_opening_hours", "outside opening hours");
    if ((startMinutes - matched.opensMin) % policy.slot_minutes !== 0) {
      throw err(422, "not_on_slot_grid", "starts_at_local is not on the slot grid");
    }
    const resolved = tz.wallToUTC(parsed.y, parsed.mo, parsed.d, parsed.h, parsed.mi, restaurant.timezone);
    if (!resolved.ok) throw err(422, "invalid_local_time", "local time does not exist");
    const startMs = resolved.utcMs;
    const endMs = startMs + duration * 60000;

    const capacity = tableIds.reduce((sum, id) => sum + policy.capacities.get(id), 0);
    if (partySize > capacity) {
      throw err(422, "party_exceeds_capacity", "party size exceeds table capacity");
    }

    return { tableIds, partySize, startsAtLocalStr, startMs, endMs, policy };
  }

  /** Structural resolution + policy validation in one call: the full pipeline for
   * a brand new reservation, or any change known in advance not to be a no-op. */
  prepareChange(restaurant, body, current) {
    const basic = this.resolveRequestedFields(restaurant, body, current);
    return this.applyPolicyValidation(restaurant, basic);
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
    const createdAtMs = Date.now();
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
      createdAtMs,
      revision: 1,
      accepted_terms: acceptedTermsOf(prepared.policy),
      history: [],
      seriesId: null,
      seriesIndex: null,
      exception: false,
    };
    recordHistory(reservation, "created", createdChangesOf(reservation), createdAtMs);
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

  /** Like findOwned, but returns null instead of throwing -- for the two read
   * endpoints (history, decision) and GET /series/{id} that collapse every
   * "not visible to this caller" case, including a missing token, to 404. */
  findOwnedOrNull(userId, reference) {
    if (!userId) return null;
    const id = this.data.referencesIndex.get(reference);
    const r = id ? this.data.reservations.get(id) : null;
    if (!r || r.user_id !== userId) return null;
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

  getReservationHistory(userId, reference) {
    const r = this.findOwnedOrNull(userId, reference);
    if (!r) throw err(404, "not_found", "unknown reservation");
    const restaurant = this.data.restaurants.get(r.restaurant_id);
    return {
      reference: r.reference,
      entries: r.history.map((h) => ({
        seq: h.seq,
        at: tz.formatInZone(h.atMs, restaurant.timezone),
        event: h.event,
        changes: h.changes,
        revision: h.revision,
        accepted_terms: h.accepted_terms,
      })),
    };
  }

  getReservationDecision(userId, reference) {
    const r = this.findOwnedOrNull(userId, reference);
    if (!r) throw err(404, "not_found", "unknown reservation");
    return { reference: r.reference, revision: r.revision, accepted_terms: r.accepted_terms };
  }

  checkCutoff(reservation) {
    const cutoffMs = reservation.accepted_terms.cancellation_cutoff_minutes * 60000;
    if (Date.now() >= reservation.startMs - cutoffMs) {
      throw err(409, "cutoff_passed", "the cancellation/amendment cutoff has passed");
    }
  }

  checkExpectedRevision(r, body) {
    if (!Object.prototype.hasOwnProperty.call(body, "expected_revision")) return;
    const v = body.expected_revision;
    if (typeof v !== "number" || !Number.isInteger(v) || v < 1) {
      throw err(422, "validation_failed", "expected_revision must be a positive integer");
    }
    if (v !== r.revision) {
      throw err(409, "stale_revision", "reservation has changed since expected_revision");
    }
  }

  /** On a real (non-no-op) change to a reservation that belongs to a series:
   * permanently flag that occurrence as a diner exception and bump the series
   * revision exactly once for this operation. */
  markSeriesExceptionIfAny(r) {
    if (!r.seriesId) return;
    const series = this.data.series.get(r.seriesId);
    if (!series) return;
    series.revision += 1;
    const occ = series.occurrences.find((o) => o.reservationId === r.id);
    if (occ) occ.exception = true;
  }

  /** On a real cancellation of a reservation that belongs to a series: bump the
   * series revision once, but do not mark an exception. */
  bumpSeriesOnCancelIfAny(r) {
    if (!r.seriesId) return;
    const series = this.data.series.get(r.seriesId);
    if (!series) return;
    series.revision += 1;
  }

  cancelReservation(userId, reference) {
    const r = this.findOwned(userId, reference);
    if (r.status === "cancelled") return this.reservationView(r);
    this.checkCutoff(r);
    r.status = "cancelled";
    r.revision += 1;
    recordHistory(r, "cancelled", []);
    this.bumpSeriesOnCancelIfAny(r);
    return this.reservationView(r);
  }

  patchReservation(userId, reference, body) {
    if (!isPlainObject(body)) throw err(400, "malformed_request", "body must be a JSON object");
    const r = this.findOwned(userId, reference);
    if (r.status === "cancelled") throw err(409, "reservation_cancelled", "reservation is cancelled");
    this.checkExpectedRevision(r, body);
    this.checkCutoff(r);
    const restaurant = this.data.restaurants.get(r.restaurant_id);
    const basic = this.resolveRequestedFields(restaurant, body, r);

    const isNoOp = sameOrderedIds(basic.tableIds, r.table_ids)
      && basic.startsAtLocalStr === r.starts_at_local
      && basic.partySize === r.party_size;
    if (isNoOp) return this.reservationView(r);

    const change = this.applyPolicyValidation(restaurant, basic);
    if (this.hasOverlapForSet(restaurant.id, change.tableIds, change.startMs, change.endMs, r.id)) {
      throw err(409, "table_unavailable", "table is not available for that interval");
    }

    const oldSnapshot = { table_ids: r.table_ids.slice(), starts_at_local: r.starts_at_local, party_size: r.party_size };
    r.table_ids = change.tableIds;
    r.party_size = change.partySize;
    r.starts_at_local = change.startsAtLocalStr;
    r.startMs = change.startMs;
    r.endMs = change.endMs;
    r.accepted_terms = acceptedTermsOf(change.policy);
    r.revision += 1;
    recordHistory(r, "changed", this.changesBetween(oldSnapshot, r));
    this.markSeriesExceptionIfAny(r);
    return this.reservationView(r);
  }

  changesBetween(oldSnap, r) {
    const changes = [];
    const oldIsPair = oldSnap.table_ids.length === 2;
    const newIsPair = r.table_ids.length === 2;
    if (!sameOrderedIds(oldSnap.table_ids, r.table_ids)) {
      if (oldIsPair || newIsPair) {
        changes.push({ field: "table_ids", from: oldSnap.table_ids.slice(), to: r.table_ids.slice() });
      } else {
        changes.push({ field: "table_id", from: oldSnap.table_ids[0], to: r.table_ids[0] });
      }
    }
    if (oldSnap.starts_at_local !== r.starts_at_local) {
      changes.push({ field: "starts_at_local", from: oldSnap.starts_at_local, to: r.starts_at_local });
    }
    if (oldSnap.party_size !== r.party_size) {
      changes.push({ field: "party_size", from: oldSnap.party_size, to: r.party_size });
    }
    return changes;
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
      this.checkExpectedRevision(r, rec.move);
      this.checkCutoff(r);
      const basic = this.resolveRequestedFields(restaurant, rec.move, r);
      const isNoOp = sameOrderedIds(basic.tableIds, r.table_ids)
        && basic.startsAtLocalStr === r.starts_at_local
        && basic.partySize === r.party_size;
      if (isNoOp) {
        prepared.push({ r, isNoOp: true, tableIds: r.table_ids, startMs: r.startMs, endMs: r.endMs });
      } else {
        const change = this.applyPolicyValidation(restaurant, basic);
        prepared.push({ r, isNoOp: false, ...change });
      }
    }

    // Batch occupancy check: the resulting set (changed and unchanged alike)
    // against itself and against every other confirmed reservation not in the batch.
    const movedIds = new Set(records.map((rec) => rec.reservation.id));
    const timeOverlap = (a, b) => a.startMs < b.endMs && b.startMs < a.endMs;
    for (let i = 0; i < prepared.length; i++) {
      const a = prepared[i];
      for (let j = 0; j < prepared.length; j++) {
        if (i === j) continue;
        const b = prepared[j];
        const sharesTable = a.tableIds.some((id) => b.tableIds.includes(id));
        if (sharesTable && timeOverlap(a, b)) {
          throw err(409, "table_unavailable", "resulting bookings overlap each other");
        }
      }
      for (const other of this.data.reservations.values()) {
        if (other.status !== "confirmed" || movedIds.has(other.id)) continue;
        if (other.restaurant_id !== restaurantId) continue;
        const sharesTable = a.tableIds.some((id) => other.table_ids.includes(id));
        if (!sharesTable) continue;
        if (a.startMs < other.endMs && other.startMs < a.endMs) {
          throw err(409, "table_unavailable", "table is not available for that interval");
        }
      }
    }

    const affectedSeriesIds = new Set();
    for (const p of prepared) {
      if (p.isNoOp) continue;
      const r = p.r;
      const oldSnapshot = { table_ids: r.table_ids.slice(), starts_at_local: r.starts_at_local, party_size: r.party_size };
      r.table_ids = p.tableIds;
      r.party_size = p.partySize;
      r.starts_at_local = p.startsAtLocalStr;
      r.startMs = p.startMs;
      r.endMs = p.endMs;
      r.accepted_terms = acceptedTermsOf(p.policy);
      r.revision += 1;
      recordHistory(r, "changed", this.changesBetween(oldSnapshot, r));
      if (r.seriesId) {
        affectedSeriesIds.add(r.seriesId);
        const series = this.data.series.get(r.seriesId);
        if (series) {
          const occ = series.occurrences.find((o) => o.reservationId === r.id);
          if (occ) occ.exception = true;
        }
      }
    }
    for (const sid of affectedSeriesIds) {
      const series = this.data.series.get(sid);
      if (series) series.revision += 1;
    }

    return prepared.map((p) => this.reservationView(p.r));
  }

  // ---- recurring series -----------------------------------------------------

  seriesView(series) {
    return {
      series_id: series.id,
      revision: series.revision,
      interval_weeks: series.interval_weeks,
      occurrences: series.occurrences.map((o) => ({
        index: o.index,
        reference: this.data.reservations.get(o.reservationId).reference,
        exception: o.exception,
        reservation: this.reservationView(this.data.reservations.get(o.reservationId)),
      })),
    };
  }

  createSeries(userId, body) {
    if (!isPlainObject(body)) throw err(400, "malformed_request", "body must be a JSON object");
    if (typeof body.anchor_reference !== "string") {
      if (body.anchor_reference === undefined) throw err(422, "validation_failed", "anchor_reference is required");
      throw err(400, "malformed_request", "anchor_reference must be a string");
    }
    const anchor = this.findOwned(userId, body.anchor_reference);
    if (anchor.status === "cancelled") throw err(409, "reservation_cancelled", "anchor is cancelled");
    this.checkCutoff(anchor);
    if (anchor.seriesId) throw err(409, "already_in_series", "reservation is already part of a series");

    const intInRange = (v, lo, hi) => typeof v === "number" && Number.isInteger(v) && v >= lo && v <= hi;
    if (!intInRange(body.count, 2, 12)) {
      throw err(422, "validation_failed", "count must be an integer 2..12");
    }
    if (!intInRange(body.interval_weeks, 1, 4)) {
      throw err(422, "validation_failed", "interval_weeks must be an integer 1..4");
    }
    const count = body.count;
    const intervalWeeks = body.interval_weeks;

    const restaurant = this.data.restaurants.get(anchor.restaurant_id);
    const anchorParsed = tz.parseLocal(anchor.starts_at_local);
    const hhmm = `${pad2(anchorParsed.h)}:${pad2(anchorParsed.mi)}`;

    const generated = [];
    for (let i = 1; i < count; i++) {
      const dd = addDaysLocal(anchorParsed.y, anchorParsed.mo, anchorParsed.d, i * intervalWeeks * 7);
      const startsAtLocalStr = `${dd.y}-${pad2(dd.mo)}-${pad2(dd.d)}T${hhmm}`;
      const body_i = { table_ids: anchor.table_ids, starts_at_local: startsAtLocalStr, party_size: anchor.party_size };
      // The first failing occurrence in index order determines the ordinary
      // booking error for the whole adoption; let it propagate as-is.
      const prepared = this.prepareChange(restaurant, body_i, null);
      if (this.hasOverlapForSet(restaurant.id, prepared.tableIds, prepared.startMs, prepared.endMs, null)) {
        throw err(409, "table_unavailable", "table is not available for that interval");
      }
      generated.push({ index: i, prepared, startsAtLocalStr });
    }

    // Every occurrence validated: commit atomically.
    const seriesId = newId("series");
    const occurrences = [{ index: 0, reservationId: anchor.id, exception: false }];
    for (const g of generated) {
      const id = newId("res");
      const reference = newReference(this.data.referencesIndex);
      const createdAtMs = Date.now();
      const reservation = {
        id,
        reference,
        user_id: userId,
        restaurant_id: restaurant.id,
        table_ids: g.prepared.tableIds,
        party_size: g.prepared.partySize,
        status: "confirmed",
        starts_at_local: g.prepared.startsAtLocalStr,
        startMs: g.prepared.startMs,
        endMs: g.prepared.endMs,
        createdAtMs,
        revision: 1,
        accepted_terms: acceptedTermsOf(g.prepared.policy),
        history: [],
        seriesId,
        seriesIndex: g.index,
        exception: false,
      };
      recordHistory(reservation, "created", createdChangesOf(reservation), createdAtMs);
      this.data.reservations.set(id, reservation);
      this.data.referencesIndex.set(reference, id);
      occurrences.push({ index: g.index, reservationId: id, exception: false });
    }
    anchor.seriesId = seriesId;
    anchor.seriesIndex = 0;
    occurrences.sort((a, b) => a.index - b.index);
    const series = { id: seriesId, user_id: userId, interval_weeks: intervalWeeks, revision: 1, occurrences };
    this.data.series.set(seriesId, series);
    return this.seriesView(series);
  }

  getSeries(userId, seriesId) {
    const series = userId ? this.data.series.get(seriesId) : null;
    if (!series || series.user_id !== userId) throw err(404, "not_found", "unknown series");
    return this.seriesView(series);
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
