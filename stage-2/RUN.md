# Running Tablekeeper — Stage 2

A single containerized HTTP + browser UI service with zero runtime dependencies
(only Node's built-ins: `http`, `crypto`, `Intl`, plus hand-written HTML/CSS/JS
served from this image). No network access is required at build time or at run
time.

## Build and start

```sh
docker build -t tablekeeper-stage2 stage-2
docker run --rm -p 8080:8080 -e PORT=8080 tablekeeper-stage2
```

The service listens on `0.0.0.0:$PORT` (default `8080`) and is ready as soon as
the container starts logging; `GET /health` returns `200 {"status":"ok"}` well
within 60 seconds.

## Try it

```sh
curl http://localhost:8080/health
```

Then open `http://localhost:8080/` in a browser:

1. Seed data with `POST /_test/reset` (same fixture shape as stage 1, plus the
   optional `combinable` field on a restaurant — see `stage-2/src/store.js` for
   the shape, or `tablekeeper/spec/stage-2.md` for the fixture example).
2. `/signup` or `/login` to get a session.
3. `/` to search a restaurant/date/party size and click an available cell
   (single-table or combined-table) to book.
4. `/lookup` to find a reservation by its confirmation reference and cancel it.

No other setup, seed step or environment variable is required.
