"use strict";

const http = require("http");
const fs = require("fs");
const path = require("path");
const { URL } = require("url");

const { World } = require("./store");
const { ApiError, err } = require("./errors");
const pages = require("./pages");

const world = new World();
const PORT = Number(process.env.PORT) || 8080;
const MAX_BODY_BYTES = 5 * 1024 * 1024;

const PUBLIC_DIR = path.join(__dirname, "public");
const STATIC_ASSETS = {
  "/app.js": { file: "app.js", type: "application/javascript; charset=utf-8" },
  "/app.css": { file: "app.css", type: "text/css; charset=utf-8" },
};
const assetCache = new Map();
function readAsset(file) {
  if (!assetCache.has(file)) {
    assetCache.set(file, fs.readFileSync(path.join(PUBLIC_DIR, file)));
  }
  return assetCache.get(file);
}

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

function sendHtml(res, status, html) {
  res.writeHead(status, {
    "Content-Type": "text/html; charset=utf-8",
    "Content-Length": Buffer.byteLength(html),
  });
  res.end(html);
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

/** For the handful of reads that collapse every non-owner case -- including a
 * missing or unknown token -- to 404, rather than the usual 401. */
function optionalAuth(req) {
  const token = extractToken(req);
  if (!token) return null;
  return world.authenticate(token);
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

function idempotent(userId, key, method, path_, parsedBody, computeFn) {
  const resolved = world.resolveIdempotency(userId, key, method, path_, parsedBody);
  if (resolved.replay) return { status: 200, body: resolved.body };
  const body = computeFn();
  world.storeIdempotency(resolved.compositeKey, resolved.requestBodyKey, body);
  return { status: 201, body };
}

// ---- API handlers -----------------------------------------------------------

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

function hListPolicies(ctx) {
  return { status: 200, body: world.listPolicies(decodeURIComponent(ctx.params[0])) };
}

function hPublishPolicy(ctx) {
  const userId = requireAuth(ctx.req);
  const key = requireIdempotencyKey(ctx.req);
  const restaurantId = decodeURIComponent(ctx.params[0]);
  const result = idempotent(userId, key, "POST", `/restaurants/${restaurantId}/policies`, ctx.body, () =>
    world.publishPolicy(userId, restaurantId, ctx.body || {}));
  return result;
}

function hAvailability(ctx) {
  const params = ctx.url.searchParams;
  const restaurant_id = requireQueryString(params, "restaurant_id");
  const date = requireQueryString(params, "date");
  const party_size = requireQueryPositiveInt(params, "party_size");
  let explain = false;
  if (params.has("explain")) {
    const v = params.get("explain");
    if (v !== "true") throw err(422, "validation_failed", "explain must be exactly 'true' if given");
    explain = true;
  }
  return { status: 200, body: world.availability({ restaurant_id, date, party_size, explain }) };
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

function hGetReservationHistory(ctx) {
  const userId = optionalAuth(ctx.req);
  return { status: 200, body: world.getReservationHistory(userId, decodeURIComponent(ctx.params[0])) };
}

function hGetReservationDecision(ctx) {
  const userId = optionalAuth(ctx.req);
  return { status: 200, body: world.getReservationDecision(userId, decodeURIComponent(ctx.params[0])) };
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

function hCreateSeries(ctx) {
  const userId = requireAuth(ctx.req);
  const key = requireIdempotencyKey(ctx.req);
  const result = idempotent(userId, key, "POST", "/series", ctx.body, () =>
    world.createSeries(userId, ctx.body || {}));
  return result;
}

function hGetSeries(ctx) {
  const userId = optionalAuth(ctx.req);
  return { status: 200, body: world.getSeries(userId, decodeURIComponent(ctx.params[0])) };
}

function hAmendSeries(ctx) {
  const userId = requireAuth(ctx.req);
  const key = requireIdempotencyKey(ctx.req);
  const seriesId = decodeURIComponent(ctx.params[0]);
  const result = idempotent(userId, key, "POST", `/series/${seriesId}/amend`, ctx.body, () =>
    world.amendSeries(userId, seriesId, ctx.body || {}));
  return result;
}

function hCreateReplan(ctx) {
  const userId = requireAuth(ctx.req);
  const key = requireIdempotencyKey(ctx.req);
  const restaurantId = decodeURIComponent(ctx.params[0]);
  const result = idempotent(userId, key, "POST", `/restaurants/${restaurantId}/replans`, ctx.body, () =>
    world.createReplan(userId, restaurantId, ctx.body || {}));
  return result;
}

function hApplyReplan(ctx) {
  const userId = requireAuth(ctx.req);
  const key = requireIdempotencyKey(ctx.req);
  const restaurantId = decodeURIComponent(ctx.params[0]);
  const planId = decodeURIComponent(ctx.params[1]);
  const result = idempotent(userId, key, "POST", `/restaurants/${restaurantId}/replans/${planId}/apply`, ctx.body, () =>
    world.applyReplan(userId, restaurantId, planId, ctx.body || {}));
  return result;
}

// ---- page / asset handlers --------------------------------------------------

function hPage(render) {
  return () => ({ status: 200, html: render() });
}

const ROUTES = [
  { method: "GET", re: /^\/health$/, kind: "api", handler: hHealth },
  { method: "POST", re: /^\/_test\/reset$/, kind: "api", body: true, handler: hReset },
  { method: "GET", re: /^\/_test\/export$/, kind: "api", handler: hExport },
  { method: "POST", re: /^\/_test\/import$/, kind: "api", body: true, handler: hImport },
  { method: "POST", re: /^\/auth\/signup$/, kind: "api", body: true, handler: hSignup },
  { method: "POST", re: /^\/auth\/login$/, kind: "api", body: true, handler: hLogin },
  { method: "GET", re: /^\/restaurants$/, kind: "api", handler: hListRestaurants },
  { method: "GET", re: /^\/restaurants\/([^/]+)$/, kind: "api", handler: hRestaurantDetail },
  { method: "GET", re: /^\/restaurants\/([^/]+)\/policies$/, kind: "api", handler: hListPolicies },
  { method: "POST", re: /^\/restaurants\/([^/]+)\/policies$/, kind: "api", body: true, handler: hPublishPolicy },
  { method: "GET", re: /^\/availability$/, kind: "api", handler: hAvailability },
  { method: "POST", re: /^\/reservations$/, kind: "api", body: true, handler: hCreateReservation },
  { method: "GET", re: /^\/reservations$/, kind: "api", handler: hListReservations },
  { method: "GET", re: /^\/reservations\/([^/]+)\/history$/, kind: "api", handler: hGetReservationHistory },
  { method: "GET", re: /^\/reservations\/([^/]+)\/decision$/, kind: "api", handler: hGetReservationDecision },
  { method: "GET", re: /^\/reservations\/([^/]+)$/, kind: "api", handler: hGetReservation },
  { method: "POST", re: /^\/reservations\/([^/]+)\/cancel$/, kind: "api", body: true, handler: hCancel },
  { method: "PATCH", re: /^\/reservations\/([^/]+)$/, kind: "api", body: true, handler: hPatch },
  { method: "POST", re: /^\/reservation-moves$/, kind: "api", body: true, handler: hMoves },
  { method: "POST", re: /^\/series$/, kind: "api", body: true, handler: hCreateSeries },
  { method: "GET", re: /^\/series\/([^/]+)$/, kind: "api", handler: hGetSeries },
  { method: "POST", re: /^\/series\/([^/]+)\/amend$/, kind: "api", body: true, handler: hAmendSeries },
  { method: "POST", re: /^\/restaurants\/([^/]+)\/replans$/, kind: "api", body: true, handler: hCreateReplan },
  { method: "POST", re: /^\/restaurants\/([^/]+)\/replans\/([^/]+)\/apply$/, kind: "api", body: true, handler: hApplyReplan },

  { method: "GET", re: /^\/$/, kind: "page", handler: hPage(pages.searchPage) },
  { method: "GET", re: /^\/signup$/, kind: "page", handler: hPage(pages.signupPage) },
  { method: "GET", re: /^\/login$/, kind: "page", handler: hPage(pages.loginPage) },
  { method: "GET", re: /^\/lookup$/, kind: "page", handler: hPage(pages.lookupPage) },
];

async function handleRequest(req, res) {
  let url;
  try {
    url = new URL(req.url, "http://placeholder");
  } catch (_e) {
    return sendError(res, err(400, "malformed_request", "invalid request URL"));
  }

  if (req.method === "GET" && STATIC_ASSETS[url.pathname]) {
    const asset = STATIC_ASSETS[url.pathname];
    try {
      const data = readAsset(asset.file);
      res.writeHead(200, { "Content-Type": asset.type, "Content-Length": data.length });
      res.end(data);
    } catch (_e) {
      res.writeHead(404);
      res.end();
    }
    return;
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
    if (matched.route.kind === "page") {
      sendHtml(res, result.status, result.html);
    } else if (result.status === 204) {
      sendNoContent(res, 204);
    } else {
      sendJson(res, result.status, result.body);
    }
  } catch (e) {
    if (matched.route.kind === "page") {
      sendHtml(res, 500, "<!doctype html><title>Error</title><p>Something went wrong.</p>");
    } else {
      sendError(res, e);
    }
  }
}

const server = http.createServer((req, res) => {
  handleRequest(req, res).catch((e) => sendError(res, e));
});

server.listen(PORT, "0.0.0.0", () => {
  // eslint-disable-next-line no-console
  console.log(`tablekeeper stage-3 listening on 0.0.0.0:${PORT}`);
});
