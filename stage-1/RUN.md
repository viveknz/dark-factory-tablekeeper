# Tablekeeper — Stage 1 — run instructions

This folder (`stage-1/`) is a self-contained HTTP service: diners can search availability, book,
cancel and amend restaurant reservations. There is no user interface yet — this stage is API only.

## Build and start

From this folder (`stage-1/`):

```sh
docker build -t tablekeeper-stage1 .
docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage1
```

The image needs no manual setup, no outbound network at runtime, and no environment variables
beyond the optional `PORT` (default `8080`). It starts and serves `GET /health` within a few
seconds, well under the 60-second budget.

Confirm it's up:

```sh
curl http://localhost:8080/health
# {"status":"ok"}
```

> Note on `--network none`: Docker does not forward published ports (`-p`) into a container
> started with `--network none` — this is a general Docker limitation (confirmed against a
> plain `nginx` image during this stage's verification, not specific to this service), not a
> defect in this image. The service itself makes no outbound network calls at startup or at
> request time: it is a single static Go binary (`CGO_ENABLED=0`) with the IANA timezone
> database compiled in (via Go's `time/tzdata`), and an in-memory store with no external
> database, cache or API client. To verify host reachability, run with the default bridge
> network as shown above; to verify no runtime egress is required, inspect the Dockerfile and
> `go.mod` — there is no code path that dials out.

## Load a fixture and try the API

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
      "tables": [{"id": "t_1", "label": "1", "capacity": 2}, {"id": "t_2", "label": "2", "capacity": 4}]
    }
  ],
  "reservations": []
}'

curl "http://localhost:8080/availability?restaurant_id=r_anker&date=2027-09-23&party_size=2"

TOKEN=$(curl -s -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" \
  -d '{"email":"ada@example.com","password":"correct horse"}' | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')

curl -X POST http://localhost:8080/reservations \
  -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: demo-1" -H "Content-Type: application/json" \
  -d '{"restaurant_id":"r_anker","table_id":"t_1","starts_at_local":"2027-09-23T19:00","party_size":2}'
```

## Project layout

```
stage-1/
  Dockerfile              multi-stage build: compile static binary, copy into a scratch image
  go.mod, go.sum
  cmd/server/main.go      entry point: reads PORT, starts the HTTP server
  internal/store/         in-memory state behind a single mutex (users, restaurants, reservations,
                           idempotency records); reset/export/import
  internal/timeutil/      restaurant-local wall-clock <-> absolute-instant resolution, including
                           spring-forward (skipped) and fall-back (ambiguous, first-occurrence) rules
  internal/httpapi/       HTTP handlers, routing, request validation, error envelope
  internal/idgen/         opaque IDs, booking references, bearer tokens
  static/                 placeholder for a later stage's screens; served at /static/*
  CHECKLIST.md            every normative spec statement, how it's verified, and its current status
```

## Running the test suite

The Go test suite (unit tests for DST resolution, and full HTTP-level tests for every
endpoint, including concurrency races and export/import round-trips) runs without Docker:

```sh
docker run --rm -v "$(pwd):/app" -w /app golang:1.23-alpine go test ./...
```

(or install Go 1.23+ locally and run `go test ./...` directly from this folder).

## Design notes for Stage 2

- Static files are served under `/static/*` from the `static/` directory next to the binary
  (`internal/httpapi/router.go`); a later stage can drop screens into `static/` without any
  server-side restructuring.
- All service state lives behind one mutex in `internal/store`; this keeps booking/idempotency
  atomicity simple and correct, at the cost of serializing writes — acceptable at the stated
  scale (50 concurrent requests, in-memory operations).
