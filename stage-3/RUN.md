# Running Tablekeeper — Stage 3

A single containerized HTTP + browser UI service with zero runtime dependencies
(only Node's built-ins: `http`, `crypto`, `Intl`, plus hand-written HTML/CSS/JS
served from this image). No network access is required at build time or at run
time.

## Build and start

```sh
docker build -t tablekeeper-stage3 stage-3
docker run --rm -p 8080:8080 -e PORT=8080 tablekeeper-stage3
```

The service listens on `0.0.0.0:$PORT` (default `8080`) and is ready as soon as
the container starts logging; `GET /health` returns `200 {"status":"ok"}` well
within 60 seconds.

## Try it

```sh
curl http://localhost:8080/health
```

Then open `http://localhost:8080/` in a browser, or drive the API directly:

1. Seed data with `POST /_test/reset` (stage-1/2 fixture shape, plus optional
   `manager_user_ids` on a restaurant — see `stage-3/src/store.js` or
   `tablekeeper/spec/stage-3.md` for the full shape).
2. A manager can publish a dated policy with
   `POST /restaurants/{id}/policies` (requires an `Idempotency-Key`).
3. `GET /availability?...&explain=true` shows why each table is or isn't
   available.
4. `GET /reservations/{reference}/history` and `/decision` show a booking's
   change log and current accepted terms.
5. `POST /series` adopts an existing reservation as a recurring agreement.

No other setup, seed step or environment variable is required.
