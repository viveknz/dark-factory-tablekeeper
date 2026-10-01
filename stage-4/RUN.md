# Running Tablekeeper — Stage 4

A single containerized HTTP + browser UI service with zero runtime dependencies
(only Node's built-ins: `http`, `crypto`, `Intl`, plus hand-written HTML/CSS/JS
served from this image). No network access is required at build time or at run
time.

## Build and start

```sh
docker build -t tablekeeper-stage4 stage-4
docker run --rm -p 8080:8080 -e PORT=8080 tablekeeper-stage4
```

The service listens on `0.0.0.0:$PORT` (default `8080`) and is ready as soon as
the container starts logging; `GET /health` returns `200 {"status":"ok"}` well
within 60 seconds.

## Try it

```sh
curl http://localhost:8080/health
```

Then open `http://localhost:8080/` in a browser, or drive the API directly:

1. Seed data with `POST /_test/reset` (stages 1-3 fixture shape, optionally
   with `manager_user_ids` and `combinable` — see `tablekeeper/spec/stage-4.md`).
2. A manager can preview a seating repair with
   `POST /restaurants/{id}/replans` (requires an `Idempotency-Key`), then
   commit it with `POST /restaurants/{id}/replans/{plan_id}/apply`.
3. A diner can bulk-retime a recurring series with
   `POST /series/{series_id}/amend`.

No other setup, seed step or environment variable is required.
