# Tablekeeper — Stage 2 — run instructions

This folder (`stage-2/`) is a self-contained HTTP service: diners can search availability,
book single or combined tables, cancel and amend reservations. It also serves this stage's
(so-far empty) static frontend assets and accepts a stage-1-shaped `/_test/import` export from
before this stage's upgrade. There is no browser UI committed by this folder yet — the frontend
builds its screens on top of this stage's commit and drops them into `static/`.

## Build and start

From this folder (`stage-2/`):

```sh
docker build -t tablekeeper-stage2 .
docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage2
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
restaurants, a combined-table booking, both demo logins) via the service's own
`POST /_test/reset`.

## Load a custom fixture and try the combined-table API

```sh
curl -X POST http://localhost:8080/_test/reset -H "Content-Type: application/json" -d '{
  "users": [
    {"id": "u_ada", "email": "ada@example.com", "password": "correct horse", "display_name": "Ada"}
  ],
  "restaurants": [
    {
      "id": "r_anker", "name": "Zum Anker", "timezone": "Europe/Berlin",
      "slot_minutes": 30, "reservation_duration_minutes": 90, "cancellation_cutoff_minutes": 120,
      "opening_hours": [{"weekday": "thu", "opens": "18:00", "closes": "23:00"}],
      "tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 4}],
      "combinable": [["t_1", "t_2"]]
    }
  ],
  "reservations": []
}'

curl "http://localhost:8080/availability?restaurant_id=r_anker&date=2027-09-23&party_size=6"
# "available_options" includes the single t_2 and the combined [t_1,t_2] pair

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" \
  -d '{"email":"ada@example.com","password":"correct horse"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

curl -X POST http://localhost:8080/reservations \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: demo-1" -H "Content-Type: application/json" \
  -d '{"restaurant_id":"r_anker","table_ids":["t_1","t_2"],"starts_at_local":"2027-09-23T19:00","party_size":6}'
```

## Project layout

```
stage-2/
  Dockerfile              multi-stage build: compile static binary, copy into a scratch image,
                           including demo/fixture.json for the optional DEMO_SEED startup path
  go.mod, go.sum
  cmd/server/main.go      entry point: reads PORT, optional DEMO_SEED, starts the HTTP server
  internal/store/         in-memory state behind a single mutex (users, restaurants incl.
                           combinable pairs, reservations incl. table_ids/status, idempotency
                           records); reset/export/import, with import accepting both this
                           stage's and stage-1's export shape
  internal/timeutil/      restaurant-local wall-clock <-> absolute-instant resolution, including
                           spring-forward (skipped) and fall-back (ambiguous, first-occurrence) rules
  internal/httpapi/       HTTP handlers, routing, request validation, error envelope, combined-
                           table booking/availability/moves logic
  internal/idgen/         opaque IDs, booking references, bearer tokens
  static/                 the frontend's build output goes here once committed; served at
                           /static/* with content-type inferred from file extension, fully offline
  demo/                   fixture.json, load_demo.py, DEMO.md, test_load_demo.py — see demo/DEMO.md
  CHECKLIST.md            every normative spec statement, how it's verified, and its current status
```

## Running the test suite

The Go test suite (unit tests for DST resolution, and full HTTP-level tests for every endpoint,
including combined-table booking/availability/moves, concurrency races, and stage-1/stage-2
export/import round-trips) runs without a local Go install, via Docker:

```sh
docker run --rm -v "$(pwd):/src" -w /src golang:1.23-alpine go vet ./... && \
docker run --rm -v "$(pwd):/src" -w /src golang:1.23-alpine go test ./...
# race detector needs cgo, so a glibc-based image for that one:
docker run --rm -v "$(pwd):/src" -w /src golang:1.23 sh -c "CGO_ENABLED=1 go test -race ./..."
```

One test, `TestImportAcceptsRealStage1Export`, builds and runs the actual accepted stage-1
binary as a subprocess to verify a real (not hand-built) stage-1 export imports and replays
correctly. It needs the sibling `stage-1/` folder visible, so mount the repo root instead of
just this folder to exercise it; it skips cleanly (not fails) otherwise:

```sh
docker run --rm -v "$(dirname $(pwd)):/src" -w /src/stage-2 golang:1.23-alpine \
  go test ./... -run TestImportAcceptsRealStage1Export -v
```

(or install Go 1.23+ locally and run the same `go vet`/`go test` commands directly from this
folder).

The demo loader has its own standard-library-only test:

```sh
python demo/test_load_demo.py
```

## Design notes

- Static files are served under `/static/*` from the `static/` directory next to the binary
  (`internal/httpapi/router.go`), via `net/http`'s `FileServer`/`http.Dir`, which infers
  content-type from file extension — HTML, CSS, JS and images all get a correct
  `Content-Type` with no extra configuration. The frontend drops its build output into
  `static/` (or documents its own subpath there) without any server-side restructuring.
- All service state lives behind one mutex in `internal/store`; this keeps single- and
  combined-table booking/idempotency atomicity simple and correct, at the cost of serializing
  writes — acceptable at the stated scale (50 concurrent requests, in-memory operations).
- `POST /_test/import` accepts both this stage's export shape and a stage-1 service's export
  shape (no `combinable` on restaurants, singular `table_id` on reservations, no `status`):
  see `internal/httpapi/stage1_export_compat_test.go`. No mid-request migration is attempted —
  only "import completes between browser requests" is supported, per spec.
