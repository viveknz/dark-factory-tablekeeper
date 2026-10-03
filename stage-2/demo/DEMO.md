# Tablekeeper demo — Stage 2

Two minutes to a working demo: two restaurants, combined tables, confirmed bookings on dates
computed from the day you run this.

## 1. Build and start the container

From `stage-2/` (this stage's folder):

```sh
docker build -t tablekeeper-stage2 .
docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage2
```

Confirm it's up: `curl http://localhost:8080/health` → `{"status":"ok"}`.

## 2. Load the demo data

With Python 3 available on your machine (the container itself needs none):

```sh
python demo/load_demo.py http://localhost:8080
```

This posts `demo/fixture.json` plus five confirmed reservations — on dates computed relative
to today, so the demo never goes stale — to the running service's own `POST /_test/reset`.
It prints what it loaded and both demo logins.

No Python on your machine? One `curl` line loads the fixture alone (no reservations):

```sh
curl -X POST http://localhost:8080/_test/reset -H "Content-Type: application/json" \
  --data-binary @demo/fixture.json
```

### Auto-seed at startup instead

Set `DEMO_SEED=1` when starting the container to load the same demo data automatically
(off by default; a later `POST /_test/reset` still fully replaces this seeded state):

```sh
docker run --rm -e PORT=8080 -e DEMO_SEED=1 -p 8080:8080 tablekeeper-stage2
```

## 3. Open the product

Open http://localhost:8080/ in a browser and sign in with either demo login below. From there:
search a restaurant/date/party size, click an available (or combined-table) slot to book it,
and look up a booking by reference at http://localhost:8080/lookup.

Prefer the API directly? It's the same data either way:

```sh
curl http://localhost:8080/restaurants
curl "http://localhost:8080/availability?restaurant_id=r_harbour_table&date=2026-10-06&party_size=4"
```

## Demo logins

| Email | Password | Display name |
|---|---|---|
| `diner@df-demo.example` | `DemoPass-2026!` | Demo Diner |
| `manager@df-demo.example` | `DemoPass-2026!` | Demo Manager (an ordinary account this stage) |

## What to try first

- In the browser: sign in as `manager@df-demo.example` at `/login`, search Harbour Table for
  the date of the Window 1 + Window 2 booking below at party size 4 — that combined-table slot
  shows as taken while the Booth and Long table still show available.
- `GET /restaurants` — "Harbour Table" (Window 1, Window 2, Booth, Long table; Window 1 +
  Window 2 are declared combinable) and "Lantern Noodle Bar" (Counter 1, Counter 2, Family).
- Log in as `diner@df-demo.example` (`POST /auth/login`) and `GET /reservations` to see demo
  diner's three confirmed bookings, including one on the Long table.
- Log in as `manager@df-demo.example` and `GET /reservations` to see a **combined-table**
  booking: Window 1 + Window 2 together at Harbour Table, party of 4.
- `GET /availability?restaurant_id=r_harbour_table&date=<that booking's date>&party_size=4` —
  the combined-table slot that's taken no longer appears in `available_options`, while the
  Booth and Long table (large enough on their own) still do.
- Cancel a reservation (`POST /reservations/{reference}/cancel` with the owner's bearer token)
  and re-check availability — the slot frees up immediately.
