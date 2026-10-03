# Tablekeeper — Stage 4 (final) — run instructions

This folder (`stage-4/`) is a self-contained HTTP service: diners can search availability
(with an optional explanation of why each table is or isn't available), book single or
combined tables under restaurant-published booking policies, cancel and amend reservations,
view a reservation's full history and current terms, arrange recurring weekly/etc. bookings and
amend them in bulk, and — new this stage — a manager can preview and apply a seating-change plan
when a table becomes unavailable, moving affected bookings onto other tables without ever
cancelling one. It serves this stage's committed frontend assets and accepts a stage-1-, -2- or
-3-shaped `/_test/import` export from before this stage's upgrade.

## Build and start

From this folder (`stage-4/`):

```sh
docker build -t tablekeeper-stage4 .
docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage4
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
via the service's own `POST /_test/reset` plus the real API, including a walkthrough of
previewing and applying a real seating-change plan.

## Load a custom fixture and try the replan/series-amend API

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

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" \
  -d '{"email":"ada@example.com","password":"correct horse"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
REF=$(curl -s -X POST http://localhost:8080/reservations \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: demo-1" -H "Content-Type: application/json" \
  -d '{"restaurant_id":"r_anker","table_id":"t_1","starts_at_local":"2027-09-23T19:00","party_size":2}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["reference"])')

MGRTOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" \
  -d '{"email":"mgr@example.com","password":"correct horse"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

# Preview closing t_1 for that evening -- Ada's booking has to move to t_2.
PLANID=$(curl -s -X POST http://localhost:8080/restaurants/r_anker/replans \
  -H "Authorization: Bearer $MGRTOKEN" -H "Idempotency-Key: plan-1" -H "Content-Type: application/json" \
  -d '{"table_id":"t_1","from":"2027-09-23T18:00:00+02:00","to":"2027-09-23T23:00:00+02:00"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["plan_id"])')

# Apply it.
curl -X POST "http://localhost:8080/restaurants/r_anker/replans/$PLANID/apply" \
  -H "Authorization: Bearer $MGRTOKEN" -H "Idempotency-Key: apply-1" -H "Content-Type: application/json" -d '{}'

# Ada's booking now shows table_id t_2, with a "reassigned" history entry naming the plan.
curl "http://localhost:8080/reservations/$REF/history" -H "Authorization: Bearer $TOKEN"

# Adopt a booking as a weekly series, then shift every occurrence from index 1 onward to 20:00:
curl -X POST http://localhost:8080/series \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: s1" -H "Content-Type: application/json" \
  -d "{\"anchor_reference\":\"$REF\",\"count\":4,\"interval_weeks\":1}"
SERIESID=... # from the response above
curl -X POST "http://localhost:8080/series/$SERIESID/amend" \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: amend-1" -H "Content-Type: application/json" \
  -d '{"expected_revision":1,"from_index":1,"local_time":"20:00"}'
```

## Project layout

```
stage-4/
  Dockerfile              multi-stage build: compile static binary, copy into a scratch image,
                           including demo/fixture.json for the optional DEMO_SEED startup path
  go.mod, go.sum
  cmd/server/main.go      entry point: reads PORT, optional DEMO_SEED, starts the HTTP server
  internal/store/         in-memory state behind a single mutex: users, restaurants (incl.
                           combinable pairs, manager_user_ids, published policies, applied
                           seating closures, a revision counter now exposed as
                           restaurant_revision), reservations (incl. table_ids/status/revision/
                           accepted_terms/history/series membership), series, seating-change
                           plans, idempotency records; reset/export/import, with import
                           accepting stage-1-, stage-2-, stage-3- and this stage's export shape
  internal/timeutil/      restaurant-local wall-clock <-> absolute-instant resolution, including
                           spring-forward (skipped) and fall-back (ambiguous, first-occurrence) rules
  internal/httpapi/       HTTP handlers, routing, request validation, error envelope; combined-
                           table booking/availability/moves/explain logic; policy selection and
                           publication; reservation history/decision; recurring series and
                           series amendment; seating-change plan preview/apply and its optimizer
  internal/idgen/         opaque IDs, booking references, bearer tokens
  static/                 the frontend's committed screens; served at /static/* with content-type
                           inferred from file extension, fully offline
  demo/                   fixture.json, load_demo.py, DEMO.md, test_load_demo.py — see demo/DEMO.md
  CHECKLIST.md            every normative spec statement, how it's verified, and its current status
```

## Running the test suite

The Go test suite (unit tests for DST resolution, and full HTTP-level tests for every endpoint,
including policies, availability explanations, reservation history, recurring series and series
amendment, seating-change plan preview/apply and its optimizer, revision-based optimistic
concurrency, and stage-1/stage-2/stage-3/stage-4 export/import round-trips) runs without a
local Go install, via Docker. Mount the **repo root**, not just this folder, so the one test
that needs the sibling `stage-1/` source tree (see below) can find it:

```sh
docker run --rm -v "$(dirname $(pwd)):/src" -w /src/stage-4 golang:1.23-alpine sh -c \
  "go vet ./... && go test ./..."
# race detector needs cgo, so a glibc-based image for that one:
docker run --rm -v "$(dirname $(pwd)):/src" -w /src/stage-4 golang:1.23 sh -c \
  "CGO_ENABLED=1 go test -race ./... -timeout 600s"
```

(`-race` is meaningfully slower than a plain run because of the real-concurrent-HTTP tests —
budget several minutes, not seconds.)

One test, `TestImportAcceptsRealStage1Export` (inherited from stage 2), builds and runs the
actual accepted stage-1 binary as a subprocess to verify a real (not hand-built) stage-1 export
imports, replays, and adopts into a series correctly. It skips cleanly (not fails) if the
sibling `stage-1/` folder isn't visible (e.g. this folder mounted on its own):

```sh
docker run --rm -v "$(dirname $(pwd)):/src" -w /src/stage-4 golang:1.23-alpine \
  go test ./... -run TestImportAcceptsRealStage1Export -v
```

(or install Go 1.23+ locally and run the same `go vet`/`go test` commands directly from the
repo root with `-C stage-4` / a `stage-4` working directory).

The demo loader has its own standard-library-only test:

```sh
python demo/test_load_demo.py
```

## Design notes

- **Seating-change plans**: `POST /restaurants/{id}/replans` is a pure, side-effect-free
  computation (`solveReplan` in `internal/httpapi/replans.go`) that exhaustively searches every
  feasible assignment of considered bookings to tables/declared pairs and picks the one
  minimizing (moved count, unused seats, rank vector) in that priority order — bounded tightly
  by the spec's own limits (≤6 tables, ≤4 pairs, ≤6 considered bookings), so exhaustive search
  is fast and, unlike a greedy heuristic, provably correct against the exact spec'd
  optimization order. A plan is only ever materialized into real state by a separate `apply`
  call; `apply` never re-optimizes — it replays the stored assignments and re-validates
  feasibility purely via the restaurant-revision staleness check (see below).
- **Why the staleness check alone is sufficient re-validation**: every operation that could
  possibly change what a plan computed at preview time (a new booking, a real amendment, a
  cancellation, a policy publication, a series adoption, a changed moves batch, or another
  plan's application) bumps `restaurant_revision` exactly once. So "the restaurant's revision is
  unchanged since preview" is both necessary and sufficient proof that nothing relevant has
  changed, and apply does not need to re-run the optimizer or re-check occupancy from scratch.
- **Revision**: each reservation carries its own optimistic-concurrency `revision`, independent
  of the restaurant's own revision counter (now exposed as `restaurant_revision` in
  replan/apply responses only — nowhere else) and a series' own revision (bumped once per real
  mutation to any of its occurrences, including a replan that moves one, but a replan never
  marks an occurrence an exception — only an individual PATCH or a reservation-moves leg does).
- **History** is append-only per reservation (`store.Reservation.History`), written by
  `RecordCreated`/`RecordChanged`/`RecordCancelled`/`RecordReassigned`, the only places history
  entries are produced; a no-op change never calls `RecordChanged`, so it never appends anything.
- Static files are served under `/static/*` from the `static/` directory next to the binary
  (`internal/httpapi/router.go`), via `net/http`'s `FileServer`/`http.Dir`.
- All service state lives behind one mutex in `internal/store`; this keeps booking/amendment/
  series/plan/idempotency atomicity simple and correct, at the cost of serializing writes —
  acceptable at the stated scale (50 concurrent requests, in-memory operations).
- `POST /_test/import` accepts stage-1-, stage-2-, stage-3- and this stage's exports: a
  reservation lacking `revision` is upgraded to revision 1 under its restaurant's policy 0, with
  a synthetic `created` history entry, exactly as if it had always been booked under policy 0
  (see `store.ImportLocked`, `internal/httpapi/stage1_export_compat_test.go`,
  `internal/httpapi/stage2_import_compat_test.go`,
  `internal/httpapi/stage3_import_compat_test.go`). No mid-request migration is attempted —
  only "import completes between browser requests" is supported, per spec.
