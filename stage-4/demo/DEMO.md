# Tablekeeper demo — Stage 4

Two minutes to a working demo: two restaurants, combined tables, confirmed bookings on dates
computed from the day you run this.

## 1. Build and start the container

From `stage-4/` (this stage's folder):

```sh
docker build -t tablekeeper-stage4 .
docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage4
```

Confirm it's up: `curl http://localhost:8080/health` → `{"status":"ok"}`.

## 2. Load the demo data

With Python 3 available on your machine (the container itself needs none):

```sh
python demo/load_demo.py http://localhost:8080
```

This posts `demo/fixture.json` plus five confirmed reservations — on dates computed relative
to today, so the demo never goes stale — to the running service's own `POST /_test/reset`, then
signs in as the demo diner through the real API and adopts one of its own bookings (the Window 1
reservation at Harbour Table) as a weekly series of 4 occurrences. It prints what it loaded and
both demo logins. If the series adoption fails for any reason, the loader logs a warning and
keeps going rather than aborting — the fixture and reservations are already loaded by that point.

No Python on your machine? One `curl` line loads the fixture alone (no reservations):

```sh
curl -X POST http://localhost:8080/_test/reset -H "Content-Type: application/json" \
  --data-binary @demo/fixture.json
```

### Auto-seed at startup instead

Set `DEMO_SEED=1` when starting the container to load the same demo data automatically
(off by default; a later `POST /_test/reset` still fully replaces this seeded state):

```sh
docker run --rm -e PORT=8080 -e DEMO_SEED=1 -p 8080:8080 tablekeeper-stage4
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
  diner's bookings, **including the 4 weekly Window 1 occurrences the loader just adopted as a
  series** — the same thing you can see in the browser via `/lookup` for any one of their
  references.
- Log in as `manager@df-demo.example` and `GET /reservations` to see a **combined-table**
  booking: Window 1 + Window 2 together at Harbour Table, party of 4.
- `GET /availability?restaurant_id=r_harbour_table&date=<that booking's date>&party_size=4` —
  the combined-table slot that's taken no longer appears in `available_options`, while the
  Booth and Long table (large enough on their own) still do.
- Cancel a reservation (`POST /reservations/{reference}/cancel` with the owner's bearer token)
  and re-check availability — the slot frees up immediately.
- A reservation's full history and current terms, as the owner (swap in a real `Bearer` token
  and reference — e.g. the diner's Window 1 reference from the series above):

  ```sh
  curl http://localhost:8080/reservations/<reference>/history -H "Authorization: Bearer <token>"
  curl http://localhost:8080/reservations/<reference>/decision -H "Authorization: Bearer <token>"
  ```

- The manager publishing a new booking policy for Harbour Table (any diner can then
  `GET /restaurants/r_harbour_table/policies` to see it, no auth required):

  ```sh
  curl -X POST http://localhost:8080/restaurants/r_harbour_table/policies \
    -H "Authorization: Bearer <manager's token>" -H "Idempotency-Key: demo-policy-1" \
    -H "Content-Type: application/json" -d '{
      "effective_from": "2026-01-01", "slot_minutes": 30,
      "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
      "opening_hours": [
        {"weekday": "mon", "opens": "17:30", "closes": "22:00"},
        {"weekday": "tue", "opens": "17:30", "closes": "22:00"},
        {"weekday": "wed", "opens": "17:30", "closes": "22:00"},
        {"weekday": "thu", "opens": "17:30", "closes": "22:00"},
        {"weekday": "fri", "opens": "17:30", "closes": "23:00"},
        {"weekday": "sat", "opens": "17:30", "closes": "23:00"},
        {"weekday": "sun", "opens": "17:30", "closes": "22:00"}
      ],
      "capacities": {"t_window_1": 2, "t_window_2": 2, "t_booth": 4, "t_long_table": 6}
    }'
  ```

## Seating changes (replans)

The diner's Window 1 booking at Harbour Table (tomorrow evening, the first occurrence of the
weekly series the loader just adopted) has room to move: Window 2, the Booth and the Long table
are all free at that same time. This walks through previewing and applying a plan that closes
Window 1 for that evening.

First, find the booking's exact (offset-aware) start/end — dates are computed relative to when
you ran the loader, so read them from the API rather than guessing:

```sh
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" \
  -d '{"email":"diner@df-demo.example","password":"DemoPass-2026!"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

read FROM TO <<EOF
$(curl -s http://localhost:8080/reservations -H "Authorization: Bearer $TOKEN" \
  | python3 -c 'import sys,json
# The loader adopted this booking as a weekly series, so several Window 1 occurrences exist;
# take the SOONEST one (not necessarily first in the list) -- "tomorrow evening".
rs = [x for x in json.load(sys.stdin)["reservations"] if x.get("table_id")=="t_window_1"]
r = min(rs, key=lambda x: x["starts_at"])
print(r["starts_at"], r["ends_at"])')
EOF
```

Then, as the manager, preview a plan closing Window 1 for exactly that window:

```sh
MGRTOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" \
  -d '{"email":"manager@df-demo.example","password":"DemoPass-2026!"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

curl -s -X POST http://localhost:8080/restaurants/r_harbour_table/replans \
  -H "Authorization: Bearer $MGRTOKEN" -H "Idempotency-Key: demo-replan-1" -H "Content-Type: application/json" \
  -d "{\"table_id\":\"t_window_1\",\"from\":\"$FROM\",\"to\":\"$TO\"}"
```

Expect a `201` with `"moved_count": 1`, one `assignments` entry for the diner's Window 1
reference with `"changed": true` and `"table_ids": ["t_window_2"]` (the only option with zero
unused seats for a party of 2), and `"unused_seats": 0`. Copy the response's `plan_id`, then
apply it:

```sh
curl -s -X POST http://localhost:8080/restaurants/r_harbour_table/replans/<plan_id>/apply \
  -H "Authorization: Bearer $MGRTOKEN" -H "Idempotency-Key: demo-replan-apply-1" -H "Content-Type: application/json" -d '{}'
```

Now `curl http://localhost:8080/reservations/<that reference>/history` (as the diner) shows a
new `reassigned` entry naming the plan, and the booking's `/lookup` page (or
`GET /reservations/<reference>`) shows it on Window 2, not Window 1 — the diner's time, party
size and accepted terms are all unchanged. A fresh `GET /availability?restaurant_id=r_harbour_table&...`
for that evening no longer offers Window 1 at all.
