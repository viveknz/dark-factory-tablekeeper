# Stage 2 checklist

Status legend: **verified** (evidence below), **failing**, **unverified** (not checked).
Evidence commands are run from `stage-2/` unless noted. Test names refer to
`internal/httpapi/*_test.go` and `internal/timeutil/dst_test.go` unless noted. Stage-1's
checklist (`stage-1/CHECKLIST.md`) covers everything stage-2 inherits unchanged; this file
covers stage-2's additions and re-confirms the inherited behaviour still holds in this folder.

## Inherited from Stage 1 (re-verified in this folder)

| # | Statement | Status | Evidence |
|---|---|---|---|
| S1 | Every stage-1 §1–§11 rule (occupancy, errors, auth, idempotency, availability grid, reservation CRUD, DST, export/import, atomic moves) still holds, unmodified behaviourally except where stage-2 explicitly widens it | verified | full `go test ./...` and `go test -race ./...` pass in `stage-2/` (see commands below); stage-1's own test files (`reservations_test.go`, `availability_test.go`, `moves_test.go`, `concurrency_test.go`, `export_import_test.go`, `auth_and_public_test.go`, `dst_http_test.go`, `internal/timeutil/dst_test.go`) carried forward unchanged and passing |
| S2 | Dockerfile + RUN.md build/start with no manual setup, no outbound network at runtime, image self-contained | verified | `docker build -t tablekeeper-stage2 .`; `docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage2`; `curl /health` → 200 in <2s |

## Combined tables — model and fixture

| # | Statement | Status | Evidence |
|---|---|---|---|
| 1 | Restaurant fixture gains `combinable: [[t_a,t_b],...]`, pairs only, not transitive | verified | `store.FixtureRestaurant.Combinable [][]string`; `validateFixture` in `testcontrol.go` rejects any entry whose length != 2; `[t_1,t_2]`+`[t_2,t_3]` never implies `{t_1,t_3}` — `resolveBookingFields` only ever checks `rest.CombinablePair(a,b)` for the exact requested pair, never derives transitively |
| 2 | Seeded reservations may carry `table_id` or `table_ids`, and `status: "cancelled"` | verified | `TestResetAcceptsSeededTableIDsAndCancelledStatus` (new test, direct `/_test/reset`, not via import): seeds one combo reservation via `table_ids` and one `status":"cancelled"` reservation via `/_test/reset` directly, confirms both read back correctly and the cancelled one's table is immediately bookable |

## `GET /availability` — `available_options`

| # | Statement | Status | Evidence |
|---|---|---|---|
| 3 | `available_table_ids` unchanged (singles only) | verified | `TestAvailabilityOptionsSinglesThenPairs`, `TestAvailabilityGridAndClosedDay` |
| 4 | `available_options`: singles (fixture order) then pairs (`combinable` order); pair `table_ids` in `combinable` order; filtered by `capacity >= party_size` and no overlapping confirmed reservation on any member | verified | `TestAvailabilityOptionsSinglesThenPairs`, `TestAvailabilityOptionsExcludesUndeclaredPair`; manual check against the demo fixture (Harbour Table, party 4, booked combo slot excluded, Booth/Long table still present) |

## `POST /reservations` / `PATCH` — combined-table bookings

| # | Statement | Status | Evidence |
|---|---|---|---|
| 5 | `table_ids` accepted; `table_id` still accepted as a 1-element set; both present → 422 `validation_failed` | verified | `TestCombinationBookingHappyPath`, `TestCombinationBookingBothFieldsIs422` |
| 6 | Response always has `table_ids`; `table_id` present iff exactly one member | verified | `TestCombinationBookingSingleTableIDResponseIncludesTableID`, `toResponse` in `reservations.go` |
| 7 | Pair not in `combinable` → 422 `combination_not_allowed` | verified | `TestCombinationErrors/undeclared_pair` |
| 8 | More than two tables → 422 `combination_not_allowed` | verified | `TestCombinationErrors/three_tables` |
| 9 | Any member table taken for an overlapping interval → 409 `table_unavailable` | verified | `TestCombinationErrors` (implicit via overlap setup), `TestConcurrentSingleVsComboOverlapOnlyOneWins` |
| 10 | `party_size` > summed capacity → 422 `party_exceeds_capacity` | verified | `TestCombinationErrors/party_exceeds_combo_capacity` |
| 11 | Duplicate table id in the set → 422 `validation_failed` | verified | `TestCombinationErrors/duplicate_table` |
| 12 | `PATCH` accepts `table_ids` under the same rules | verified | `TestAmendBetweenSingleAndCombo` |
| 13 | Cancelling a combo frees every table in the set | verified | `TestCancelComboFreesBothTables` |

## `POST /reservation-moves` — combined-table legs

| # | Statement | Status | Evidence |
|---|---|---|---|
| 14 | Accepts `table_ids` per move | verified | `TestReservationMovesWithComboLeg` |
| 15 | No table belongs to overlapping resulting bookings (across the whole batch, combo legs included) | verified | `TestReservationMovesWithComboLeg`, `TestReservationMovesOverlapRejectsWholeBatch` |

## Concurrency (combined tables)

| # | Statement | Status | Evidence |
|---|---|---|---|
| 16 | Concurrent requests for the same combination give the same result as some serial order, at every read | verified | `TestConcurrentComboBookingOnlyOneWins` (two clients race for the same pair, exactly one wins), `TestConcurrentSingleVsComboOverlapOnlyOneWins` (a single-table booking and a combo sharing a table race); `go test -race ./...` clean |

## Existing clients after an upgrade

| # | Statement | Status | Evidence |
|---|---|---|---|
| 17 | A stage-2 service accepts a stage-1-shaped export (no `combinable` field, singular `table_id`, no `status`) | verified | `TestImportAcceptsStage1ShapedExport`, `TestImportAcceptsStage1RestaurantWithoutCombinableField` |
| 18 | Bearer tokens from the stage-1 export keep working after import | verified | `TestImportAcceptsStage1ShapedExport` (legacy token lists the imported reservation) |
| 19 | Retained booking reference still works through direct lookup after import | verified | `TestImportAcceptsStage1ShapedExport` (`GET /reservations/LEGACY1`) |
| 20 | A lost-response retry (same key+body) from before the export completes correctly after import, returning the original response, no duplicate booking | verified | `TestImportAcceptsStage1ShapedExport` (idempotency replay returns `LEGACY1` with 200, reservation count stays at 1, and — fixed after an independent-review finding — the replayed body now carries `table_ids` just like a live `GET` on the same reservation, via `store.upgradeLegacyResponseBody` applied at import) |
| 21 | No mid-request migration required — only import-between-requests | verified (by design) | import is a single atomic `ImportLocked` call under the store mutex; no code path attempts migration during an in-flight request |

## Static file serving

| # | Statement | Status | Evidence |
|---|---|---|---|
| 22 | Serves the frontend's static files (once committed) with correct content-types, fully offline | verified (mechanism only — no frontend files committed yet) | `internal/httpapi/router.go`: `http.FileServer(http.Dir("static"))` mounted at `/static/*`, `http.FileServer` infers `Content-Type` from file extension via `net/http`'s built-in table, no network call; `cmd/server/main.go` only mounts it if the `static` directory exists, so the route degrades gracefully pre-frontend |

## Demo

| # | Statement | Status | Evidence |
|---|---|---|---|
| 23 | `demo/fixture.json`: both demo users (`DemoPass-2026!`), both restaurants (Harbour Table w/ Window1+Window2 combinable, Lantern Noodle Bar), exact tables/hours/durations/cutoffs from the dispatch | verified | file inspection; `TestResetRejectsIDsLongerThan64Chars`-style validation passes on load (no 422 from `/_test/reset`); manual `docker run` + `python demo/load_demo.py` round-trip (see below) |
| 24 | `demo/load_demo.py`: stdlib-only, posts fixture + future-dated confirmed reservations (computed from run day) to `/_test/reset`, prints logins, logs each step, exits non-zero with a clear message on failure | verified | `python demo/test_load_demo.py` (`test_happy_path`, `test_unreachable_server_fails_loudly`); manual run against a live `tablekeeper-stage2` container — printed both logins, 5 reservations landed, diner/manager logins both see their bookings including the Window1+Window2 combo |
| 25 | `demo/DEMO.md`: exact build/start/load/open commands, both logins, what to try first, plus a Python-less `curl` one-liner for the fixture alone | verified | file inspection; every command in it was run manually during this verification pass (build, run, load_demo.py, curl one-liner, login, availability, cancel) |
| 26 | `DEMO_SEED=1` auto-loads the same demo at startup, off by default, does not change `/_test/reset`'s own behavior | verified | `docker run -e DEMO_SEED=1 ...` then `GET /reservations` for both demo users shows the seeded bookings without running the loader; a subsequent `python demo/load_demo.py` against the same container still fully replaces state (5 reservations total, not 10) |
| 27 | A small automated test for the loader | verified | `demo/test_load_demo.py`, run via `python demo/test_load_demo.py` (standard library only, no outbound network — uses a local `HTTPServer` on an OS-assigned loopback port) |

## Verification commands run this stage

```
docker build -t tablekeeper-stage2 .
docker run --rm -v "<repo>/stage-2:/src" -w /src golang:1.23-alpine go vet ./...
docker run --rm -v "<repo>/stage-2:/src" -w /src golang:1.23-alpine go test ./...
docker run --rm -v "<repo>/stage-2:/src" -w /src golang:1.23 sh -c "CGO_ENABLED=1 go test -race ./..."
python demo/test_load_demo.py
docker run -d --name tk2test -e PORT=8080 -e DEMO_SEED=1 -p 18080:8080 tablekeeper-stage2
curl http://localhost:18080/health
curl http://localhost:18080/restaurants
python demo/load_demo.py http://localhost:18080
# login as both demo users, GET /reservations, GET /availability around the combo booking
docker rm -f tk2test
python -m harness run --track tablekeeper --repo <repo> --stage 2 --mode host --out <out>
python -m harness check <repo> --track tablekeeper
```

## Unverified / out of scope for this seat

- UI/screen-level requirements (routes, `data-testid` hooks, competing-clients browser
  behaviour, visual system, `DESIGN.md`/`NOTES.md`) — owned by `@df-frontend`, not checked here.
- Load/latency testing beyond the concurrency tests above (50 concurrent in flight, 5s/10s
  timeouts) — not re-measured this stage; stage-1's reasoning (in-memory, O(n) over small
  fixtures) still applies, combined-table checks add only a small constant factor.
