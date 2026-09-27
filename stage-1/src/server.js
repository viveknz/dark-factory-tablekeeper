"use strict";

const http = require("http");
const { URL } = require("url");

const { World } = require("./store");
const { ApiError, err } = require("./errors");

const world = new World();
const PORT = Number(process.env.PORT) || 8080;
const MAX_BODY_BYTES = 5 * 1024 * 1024;

function sendJson(res, status, body) {
  const text = JSON.stringify(body);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Content-Length": Buffer.byteLength(text),
  });
  res.end(text);
}

function sendNoContent(res, status) {
  res.writeHead(status, { "Content-Length": 0 });
  res.end();
}

function sendError(res, e) {
  const status = e instanceof ApiError ? e.status : 422;
  const code = e instanceof ApiError ? e.code : "validation_failed";
  const message = e instanceof ApiError ? e.message : "unexpected error";
  sendJson(res, status, { error: { code, message } });
}

function readBody(req) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let size = 0;
    req.on("data", (chunk) => {
      size += chunk.length;
      if (size > MAX_BODY_BYTES) {
        reject(err(400, "malformed_request", "request body too large"));
        req.destroy();
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
    req.on("error", (e) => reject(e));
  });
}

function parseJsonBody(raw) {
  const trimmed = raw.trim();
  if (trimmed === "") return {};
  try {
    return JSON.parse(trimmed);
  } catch (_e) {
    throw err(400, "malformed_request", "request body is not valid JSON");
  }
}

function extractToken(req) {
  const h = req.headers["authorization"];
  if (typeof h !== "string") return null;
  const m = /^Bearer\s+(.+)$/.exec(h);
  return m ? m[1] : null;
}

function requireAuth(req) {
  const token = extractToken(req);
  if (!token) throw err(401, "unauthenticated", "missing bearer token");
  const userId = world.authenticate(token);
  if (!userId) throw err(401, "unauthenticated", "unknown bearer token");
  return userId;
}

function requireIdempotencyKey(req) {
  const key = req.headers["idempotency-key"];
  if (typeof key !== "string" || key.length === 0) {
    throw err(400, "missing_idempotency_key", "Idempotency-Key header is required");
  }
  if (key.length > 255) {
    throw err(422, "validation_failed", "Idempotency-Key must be 1 to 255 characters");
  }
  return key;
}

function requireQueryString(params, name) {
  const v = params.get(name);
  if (v === null || v === "") throw err(422, "validation_failed", `${name} is required`);
  return v;
}

function requireQueryPositiveInt(params, name) {
  const v = requireQueryString(params, name);
  if (!/^\d+$/.test(v)) {
    throw err(422, "validation_failed", `${name} must be plain decimal digits`);
  }
  const n = Number(v);
  if (!Number.isSafeInteger(n) || n < 1) {
    throw err(422, "validation_failed", `${name} must be a positive integer`);
  }
  return n;
}

function idempotent(userId, key, method, path, parsedBody, computeFn) {
  const resolved = world.resolveIdempotency(userId, key, method, path, parsedBody);
  if (resolved.replay) return { status: 200, body: resolved.body };
  const body = computeFn();
  world.storeIdempotency(resolved.compositeKey, resolved.requestBodyKey, body);
  return { status: 201, body };
}

// ---- route handlers ---------------------------------------------------------

function hHealth() {
  return { status: 200, body: { status: "ok" } };
}

function hReset(ctx) {
  world.reset(ctx.body);
  return { status: 204 };
}

function hExport() {
  return { status: 200, body: world.exportState() };
}

function hImport(ctx) {
  world.importState(ctx.body);
  return { status: 204 };
}

function hSignup(ctx) {
  return { status: 201, body: world.signup(ctx.body || {}) };
}

function hLogin(ctx) {
  return { status: 200, body: world.login(ctx.body || {}) };
}

function hListRestaurants() {
  return { status: 200, body: { restaurants: world.listRestaurants() } };
}

function hRestaurantDetail(ctx) {
  return { status: 200, body: world.getRestaurantDetail(decodeURIComponent(ctx.params[0])) };
}

function hAvailability(ctx) {
  const restaurant_id = requireQueryString(ctx.url.searchParams, "restaurant_id");
  const date = requireQueryString(ctx.url.searchParams, "date");
  const party_size = requireQueryPositiveInt(ctx.url.searchParams, "party_size");
  return { status: 200, body: world.availability({ restaurant_id, date, party_size }) };
}

function hCreateReservation(ctx) {
  const userId = requireAuth(ctx.req);
  const key = requireIdempotencyKey(ctx.req);
  const result = idempotent(userId, key, "POST", "/reservations", ctx.body, () =>
    world.createReservation(userId, ctx.body || {}));
  return result;
}

function hListReservations(ctx) {
  const userId = requireAuth(ctx.req);
  return { status: 200, body: { reservations: world.listReservations(userId) } };
}

function hGetReservation(ctx) {
  const userId = requireAuth(ctx.req);
  return { status: 200, body: world.getReservation(userId, decodeURIComponent(ctx.params[0])) };
}

function hCancel(ctx) {
  const userId = requireAuth(ctx.req);
  return { status: 200, body: world.cancelReservation(userId, decodeURIComponent(ctx.params[0])) };
}

function hPatch(ctx) {
  const userId = requireAuth(ctx.req);
  return { status: 200, body: world.patchReservation(userId, decodeURIComponent(ctx.params[0]), ctx.body || {}) };
}

function hMoves(ctx) {
  const userId = requireAuth(ctx.req);
  const key = requireIdempotencyKey(ctx.req);
  const result = idempotent(userId, key, "POST", "/reservation-moves", ctx.body, () => ({
    reservations: world.createMoves(userId, ctx.body || {}),
  }));
  return result;
}

const ROUTES = [
  { method: "GET", re: /^\/health$/, handler: hHealth, body: false },
  { method: "POST", re: /^\/_test\/reset$/, handler: hReset, body: true },
  { method: "GET", re: /^\/_test\/export$/, handler: hExport, body: false },
  { method: "POST", re: /^\/_test\/import$/, handler: hImport, body: true },
  { method: "POST", re: /^\/auth\/signup$/, handler: hSignup, body: true },
  { method: "POST", re: /^\/auth\/login$/, handler: hLogin, body: true },
  { method: "GET", re: /^\/restaurants$/, handler: hListRestaurants, body: false },
  { method: "GET", re: /^\/restaurants\/([^/]+)$/, handler: hRestaurantDetail, body: false },
  { method: "GET", re: /^\/availability$/, handler: hAvailability, body: false },
  { method: "POST", re: /^\/reservations$/, handler: hCreateReservation, body: true },
  { method: "GET", re: /^\/reservations$/, handler: hListReservations, body: false },
  { method: "GET", re: /^\/reservations\/([^/]+)$/, handler: hGetReservation, body: false },
  { method: "POST", re: /^\/reservations\/([^/]+)\/cancel$/, handler: hCancel, body: true },
  { method: "PATCH", re: /^\/reservations\/([^/]+)$/, handler: hPatch, body: true },
  { method: "POST", re: /^\/reservation-moves$/, handler: hMoves, body: true },
];

async function handleRequest(req, res) {
  let url;
  try {
    url = new URL(req.url, "http://placeholder");
  } catch (_e) {
    return sendError(res, err(400, "malformed_request", "invalid request URL"));
  }

  let matched = null;
  let allowedMethodsForPath = [];
  for (const route of ROUTES) {
    const m = route.re.exec(url.pathname);
    if (m) {
      allowedMethodsForPath.push(route.method);
      if (route.method === req.method) {
        matched = { route, params: m.slice(1) };
        break;
      }
    }
  }

  if (!matched) {
    if (allowedMethodsForPath.length > 0) {
      return sendError(res, err(404, "not_found", "method not supported on this path"));
    }
    return sendError(res, err(404, "not_found", "no such resource"));
  }

  try {
    let body;
    if (matched.route.body) {
      const raw = await readBody(req);
      body = parseJsonBody(raw);
    }
    const ctx = { req, url, params: matched.params, body };
    const result = matched.route.handler(ctx);
    if (result.status === 204) sendNoContent(res, 204);
    else sendJson(res, result.status, result.body);
  } catch (e) {
    sendError(res, e);
  }
}

const server = http.createServer((req, res) => {
  handleRequest(req, res).catch((e) => sendError(res, e));
});

server.listen(PORT, "0.0.0.0", () => {
  // eslint-disable-next-line no-console
  console.log(`tablekeeper stage-1 listening on 0.0.0.0:${PORT}`);
});
