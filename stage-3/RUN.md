# Tablekeeper — Stage 3 — run instructions

This folder (`stage-3/`) is a self-contained HTTP service: diners can search availability
(with an optional explanation of why each table is or isn't available), book single or
combined tables under restaurant-published booking policies, cancel and amend reservations,
view a reservation's full history and current terms, and arrange recurring weekly/etc.
bookings. It serves this stage's committed frontend assets and accepts a stage-1- or
stage-2-shaped `/_test/import` export from before this stage's upgrade.

## Build and start

From this folder (`stage-3/`):

```sh
docker build -t tablekeeper-stage3 .
docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage3
```

The image needs no manual setup, no outbound network at runtime, and no environment variables
beyond the optional `PORT` (default `8080`) and `DEMO_SEED` (see `demo/DEMO.md`). It starts and
serves `GET /health` within a few seconds, well under the 60-second budget.

Confirm it's up:

```sh
curl http://localhost:8080/health
# {"status":"ok"}
```

> Note on `--network none`: Docker does not forward published ports (`-p`) into a container
> started with `--network none` — a general Docker limitation (confirmed against a plain
> `nginx` image during stage-1's verification), not a defect in this image. The service makes
> no outbound network calls at startup or request time: a single static Go binary
> (`CGO_ENABLED=0`) with the IANA timezone database compiled in (`time/tzdata`), an in-memory
> store, and no external database/cache/API client. Run with the default bridge network to
> verify host reachability; inspect the Dockerfile/`go.mod` to verify no runtime egress path
> exists.

## Try the demo

See `demo/DEMO.md` for the exact commands to load two-minutes-to-working demo data (two
restaurants, a combined-table booking, a diner's weekly reservation series, both demo logins)
via the service's own `POST /_test/reset` plus the real API, and for curl examples covering
history/decision and publishing a policy.

## Load a custom fixture and try the policy/explain/history/series API

```sh
curl -X POST http://localhost:8080/_test/reset -H "Content-Type: application/json" -d '{
  "users": [
    {"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"},
    {"id": "u_mgr", "email": "mgr@example.com", "password": "correct horse", "display_name": "Mgr"}
  ],
  "restaurants": [
    {
      "id": "r_anker", "name": "Zum Anker", "timezone": "Europe/Berlin",
      "slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
      "opening_hours": [{"weekday": "thu", "opens": "18:00", "closes": "23:00"}],
      "tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 4}],
      "combinable": [["t_1", "t_2"]],
      "manager_user_ids": ["u_mgr"]
    }
  ],
  "reservations": []
}'

# A table's availability explained:
curl "http://localhost:8080/availability?restaurant_id=r_anker&date=2027-09-23&party_size=6&explain=true"

MGRTOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" \
  -d '{"email":"mgr@example.com","password":"correct horse"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

# The manager publishes a policy (tightens capacity on t_1):
curl -X POST http://localhost:8080/restaurants/r_anker/policies \
  -H "Authorization: Bearer $MGRTOKEN" -H "Idempotency-Key: p1" -H "Content-Type: application/json" -d '{
    "effective_from": "2027-01-01", "slot_minutes": 30, "reservation_duration_minutes": 90,
    "cancellation_cutoff_minutes": 120,
    "opening_hours": [{"weekday": "thu", "opens": "18:00", "closes": "23:00"}],
    "capacities": {"t_1": 1, "t_2": 4}
  }'

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" \
  -d '{"email":"ada@example.com","password":"correct horse"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

REF=$(curl -s -X POST http://localhost:8080/reservations \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: demo-1" -H "Content-Type: application/json" \
  -d '{"restaurant_id":"r_anker","table_ids":["t_1","t_2"],"starts_at_local":"2027-09-23T19:00","party_size":6}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["reference"])')

# Its history and current terms:
curl "http://localhost:8080/reservations/$REF/history" -H "Authorization: Bearer $TOKEN"
curl "http://localhost:8080/reservations/$REF/decision" -H "Authorization: Bearer $TOKEN"

# Adopt it as a weekly series of 4:
curl -X POST http://localhost:8080/series \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: s1" -H "Content-Type: application/json" \
  -d "{\"anchor_reference\":\"$REF\",\"count\":4,\"interval_weeks\":1}"
```

## Project layout

```
stage-3/
  Dockerfile              multi-stage build: compile static binary, copy into a scratch image,
                           including demo/fixture.json for the optional DEMO_SEED startup path
  go.mod, go.sum
  cmd/server/main.go      entry point: reads PORT, optional DEMO_SEED, starts the HTTP server
  internal/store/         in-memory state behind a single mutex: users, restaurants (incl.
                           combinable pairs, manager_user_ids, published policies, an internal
                           revision counter), reservations (incl. table_ids/status/revision/
                           accepted_terms/history/series membership), series, idempotency
                           records; reset/export/import, with import accepting stage-1-, stage-2-
                           and this stage's export shape
  internal/timeutil/      restaurant-local wall-clock <-> absolute-instant resolution, including
                           spring-forward (skipped) and fall-back (ambiguous, first-occurrence) rules
  internal/httpapi/       HTTP handlers, routing, request validation, error envelope; combined-
                           table booking/availability/moves/explain logic; policy selection and
                           publication; reservation history/decision; recurring series
  internal/idgen/         opaque IDs, booking references, bearer tokens
  static/                 the frontend's committed screens; served at /static/* with content-type
                           inferred from file extension, fully offline
  demo/                   fixture.json, load_demo.py, DEMO.md, test_load_demo.py — see demo/DEMO.md
  CHECKLIST.md            every normative spec statement, how it's verified, and its current status
```

## Running the test suite

The Go test suite (unit tests for DST resolution, and full HTTP-level tests for every endpoint,
including policies, availability explanations, reservation history, recurring series, revision-
based optimistic concurrency, and stage-1/stage-2/stage-3 export/import round-trips) runs
without a local Go install, via Docker. Mount the **repo root**, not just this folder, so the
one test that needs the sibling `stage-1/` source tree (see below) can find it:

```sh
docker run --rm -v "$(dirname $(pwd)):/src" -w /src/stage-3 golang:1.23-alpine sh -c \
  "go vet ./... && go test ./..."
# race detector needs cgo, so a glibc-based image for that one:
docker run --rm -v "$(dirname $(pwd)):/src" -w /src/stage-3 golang:1.23 sh -c \
  "CGO_ENABLED=1 go test -race ./... -timeout 600s"
```

(`-race` is meaningfully slower than a plain run because of the real-concurrent-HTTP tests —
budget several minutes, not seconds.)

One test, `TestImportAcceptsRealStage1Export` (inherited from stage 2), builds and runs the
actual accepted stage-1 binary as a subprocess to verify a real (not hand-built) stage-1 export
imports, replays, and now also adopts into a series correctly. It skips cleanly (not fails) if
the sibling `stage-1/` folder isn't visible (e.g. this folder mounted on its own):

```sh
docker run --rm -v "$(dirname $(pwd)):/src" -w /src/stage-3 golang:1.23-alpine \
  go test ./... -run TestImportAcceptsRealStage1Export -v
```

(or install Go 1.23+ locally and run the same `go vet`/`go test` commands directly from the
repo root with `-C stage-3` / a `stage-3` working directory).

The demo loader has its own standard-library-only test:

```sh
python demo/test_load_demo.py
```

## Design notes

- **Policies**: a restaurant's bookable rules (slot grid, duration, cutoff, opening hours,
  per-table capacity) are resolved per booking via `Restaurant.SelectPolicy(date)`, never read
  directly off the restaurant's raw fixture fields except as "policy 0" (the implicit baseline).
  Every reservation snapshots the policy it was accepted under (`accepted_terms`) so later
  publications never retroactively change an existing booking's rules, cutoff or capacity.
- **Revision**: each reservation carries its own optimistic-concurrency `revision`, independent
  of a restaurant-level internal revision counter (bumped once per series adoption or per
  reservation-moves batch with a real change) and a series' own revision (bumped once per real
  mutation to any of its occurrences). None of the restaurant-level counter is exposed in any
  response — the spec asks only that it be tracked, not surfaced.
- **History** is append-only per reservation (`store.Reservation.History`), written by
  `RecordCreated`/`RecordChanged`/`RecordCancelled`, which are the only places history entries
  are produced; a no-op amendment never calls `RecordChanged`, so it never appends anything.
- Static files are served under `/static/*` from the `static/` directory next to the binary
  (`internal/httpapi/router.go`), via `net/http`'s `FileServer`/`http.Dir`.
- All service state lives behind one mutex in `internal/store`; this keeps booking/amendment/
  series/idempotency atomicity simple and correct, at the cost of serializing writes —
  acceptable at the stated scale (50 concurrent requests, in-memory operations).
- `POST /_test/import` accepts stage-1-, stage-2- and stage-3-shaped exports: a reservation
  lacking `revision` is upgraded to revision 1 under its restaurant's policy 0, with a synthetic
  `created` history entry, exactly as if it had always been booked under policy 0 (see
  `store.ImportLocked`, `internal/httpapi/stage1_export_compat_test.go`,
  `internal/httpapi/stage2_import_compat_test.go`). No mid-request migration is attempted — only
  "import completes between browser requests" is supported, per spec.
