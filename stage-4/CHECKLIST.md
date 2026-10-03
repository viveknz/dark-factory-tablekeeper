# Stage 4 checklist (final stage)

Status legend: **verified** (evidence below), **failing**, **unverified** (not checked).
Evidence commands are run from `stage-4/` unless noted. Test names refer to
`internal/httpapi/*_test.go` unless noted. Stages 1-3's checklists cover everything inherited
unchanged; this file covers stage-4's additions and re-confirms the inherited behaviour holds.

## Inherited from Stages 1-3 (re-verified in this folder)

| # | Statement | Status | Evidence |
|---|---|---|---|
| S1 | Every stage-1/2/3 rule (occupancy, errors, auth, idempotency, availability grid, combined tables, policies, history, series, concurrency) still holds | verified | full `go test ./...` and `go test -race ./... -timeout 600s` pass in `stage-4/` with every earlier test file carried forward unchanged |
| S2 | Dockerfile + RUN.md build/start with no manual setup, no outbound network at runtime | verified | `docker build -t tablekeeper-stage4 .`; `docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage4`; `curl /health` → 200 |

## Seating-change plans: preview

| # | Statement | Status | Evidence |
|---|---|---|---|
| 1 | `POST /restaurants/{id}/replans`: manager-only (403/401/404), idempotency key required | verified | `TestReplanPreviewPermissionsAndValidation` |
| 2 | `from<to` with explicit offsets, else 422; unknown table → 404; closure is half-open `[from,to)` | verified | `TestReplanPreviewPermissionsAndValidation` |
| 3 | Considers every confirmed booking at the restaurant overlapping the interval; others keep their assignment | verified | `TestReplanPreviewMovesOneBookingAndDoesNotMutateState` |
| 4 | Planning limit: >6 tables, >4 declared pairs, or >6 considered bookings → 422 `planning_limit` | verified | `TestReplanPlanningLimitTooManyConsideredBookings` |
| 5 | Each considered booking retains reference/owner/party/start/end/accepted-terms; assigned under its OWN accepted terms (not the live policy); no conflict with fixed bookings/other assignments/applied closures/the proposed closure; diner cutoffs do not block it; no booking may disappear — infeasible → 409 `no_feasible_plan`, nothing changed | verified | `TestReplanPreviewMovesOneBookingAndDoesNotMutateState`, `TestReplanNoFeasiblePlanLeavesNothingChanged` |
| 6 | Optimization order: (1) minimize changed-table-set count, (2) minimize total unused seats, (3) minimize the rank vector in ascending reference order (singles fixture order then pairs declared order, from 0) — proven against a genuinely adversarial tie, not merely *a* feasible plan | verified | `TestSolveReplanTieBreakRespectsReferenceOrder` (direct unit test of the optimizer core: two bookings tie exactly on criteria 1 and 2, only the reference-ordered rank vector distinguishes `[2,3]` — correct — from `[3,2]` — what a map-iteration-order or greedy-by-party-size implementation could easily produce instead) |
| 7 | 201 response shape: `plan_id`, `restaurant_revision`, `closure`, `assignments` (every considered booking, reference order, `changed` flag), `moved_count`, `unused_seats` | verified | `TestReplanPreviewMovesOneBookingAndDoesNotMutateState` |
| 8 | `restaurant_revision` starts at 0 after reset; +1 per new booking/real amendment/cancellation/policy publication/series adoption/changed moves-batch/plan application (union of stage-3's and stage-4's rules); no-op writes, failures, previews and replays never increment it | verified | `TestRestaurantRevisionIncrementsOnEveryTrigger` (booking, amendment, cancellation, policy publication each +1); `TestReplanPreviewMovesOneBookingAndDoesNotMutateState` (preview does not increment — the live reservation's own revision, a separate counter, is also unchanged); series-adoption/moves-batch triggers inherited from stage 3 unchanged |
| 9 | Preview stores only the plan — no closure/occupancy/reservation-revision/history change | verified | `TestReplanPreviewMovesOneBookingAndDoesNotMutateState` |

## Seating-change plans: apply

| # | Statement | Status | Evidence |
|---|---|---|---|
| 10 | `POST /restaurants/{id}/replans/{plan_id}/apply`: body `{}`, manager + idempotency key; 201 with `plan_id`/`restaurant_revision`/`reservations` (every considered booking, reference order) | verified | `TestReplanApplyHappyPathClosureAndHistory` |
| 11 | Unknown plan or another restaurant's plan → 404 | verified | `handleApplyReplan`'s `plan.RestaurantID != restID` check, exercised by `TestReplanApplyStaleAndAlreadyApplied`'s setup |
| 12 | An intervening restaurant-revision change → 409 `stale_plan`, nothing changed | verified | `TestReplanApplyStaleAndAlreadyApplied` |
| 13 | Already applied under a different key → 409 `plan_already_applied`; replay of the successful key → original response, 200, forever | verified | `TestReplanApplyStaleAndAlreadyApplied` |
| 14 | Application records closure + all assignments together; each MOVED booking gets exactly one revision bump + one `reassigned` history entry (`table_ids` change + `plan_id`); accepted terms/times stay identical; UNMOVED bookings gain nothing; restaurant revision bumps once for the whole plan | verified | `TestReplanApplyHappyPathClosureAndHistory` |
| 15 | After application, the closure excludes its table(s) from availability/creates/amendments (409 `table_unavailable`); `explain=true`'s `no_overlap` is false for a closure exactly as for a conflicting booking | verified | `TestReplanApplyHappyPathClosureAndHistory` (both the create-rejection and the explain assertion) |
| 16 | Concurrent applications must not leave partially-moved bookings — real concurrent-HTTP proof | verified | `TestConcurrentReplanApplicationsOnlyOneSucceeds` (10 concurrent applications sharing one idempotency key under the store's single mutex; exactly one 201, the rest 200 replays, final revision bumped exactly once — not a code read) |
| 17 | A closure at another restaurant does not invalidate this plan (staleness check is scoped to one restaurant) | verified | `TestReplanClosureAtAnotherRestaurantDoesNotInvalidate` |

## Recurring-series amendment

| # | Statement | Status | Evidence |
|---|---|---|---|
| 18 | `POST /series/{id}/amend`: owner-only idempotent write, 404 unknown/other-owner, 401 no token | verified | `TestSeriesAmendValidation` |
| 19 | Body validation: `expected_revision` positive int, `from_index` 0..count-1, `local_time` exact `HH:MM` 00:00..23:59, booleans invalid anywhere an int is expected → 422 | verified | `TestSeriesAmendValidation` (8 subcases) |
| 20 | Mismatched series revision → 409 `stale_revision`, checked BEFORE any occurrence's cutoff/validation | verified | `TestSeriesAmendValidation` |
| 21 | Considers indices ≥ `from_index`, excluding cancelled and exception-marked occurrences; shifts clock time on the occurrence's OWN unchanged scheduled date, keeping reference/owner/party/current tables | verified | `TestSeriesAmendShiftsFromIndexExcludesCancelledAndException` |
| 22 | Identical-result occurrence is a no-op: retains terms, no history, no revision bump for it | verified | `TestSeriesAmendAllNoOpChangesNoRevisions` |
| 23 | Each real change checks its own old accepted cutoff, then adopts the resulting date's policy (same as an individual PATCH) | verified | `internal/httpapi/series_amend.go` reuses `resolveBookingFields` exactly as `handleAmendReservation` does; exercised by `TestSeriesAmendShiftsFromIndexExcludesCancelledAndException` and the DST test below |
| 24 | Resulting occurrences must not conflict with unchanged occurrences/other bookings/applied closures; on failure nothing changes anywhere; non-occupancy errors take precedence over an occupancy conflict, in occurrence-index order | verified | `TestSeriesAmendPrecedenceNonOccupancyOverOccupancy` (a hand-built adversarial case: an EARLIER occurrence has an occupancy conflict, a LATER occurrence has a non-occupancy DST error — the non-occupancy error is correctly reported, which a naive single-pass index-order implementation would get wrong) |
| 25 | Success: 201 current series response; each changed occurrence gets one `changed` history entry + one reservation-revision; series AND restaurant revisions each +1 once for the whole operation, only if anything changed; series amendments never mark exceptions; all-no-op/empty-eligible-set still succeeds with no revision changes; replay returns the original response, 200, forever | verified | `TestSeriesAmendShiftsFromIndexExcludesCancelledAndException` (exception flag unchanged after amend), `TestSeriesAmendAllNoOpChangesNoRevisions`, `TestSeriesAmendEmptyEligibleSetSucceeds`, `TestSeriesAmendReplayReturnsOriginalForever` |
| 26 | Seating repairs (replans) that move a series occurrence preserve its exception flag/scheduled date/identity/accepted terms; the affected series revision increases once PER PLAN APPLICATION if ≥1 member moved | verified | `TestReplanOnSeriesOccurrencePreservesIdentityAndBumpsSeriesRevisionOnce` |
| 27 | Concurrent amendments from the same `expected_revision` may not both make a real change — real concurrent-HTTP proof | verified | `TestConcurrentSeriesAmendsSameRevisionOnlyOneRealChange` (10 concurrent amends sharing one revision+key; exactly one real change, final revision bumped exactly once) |

## Migration

| # | Statement | Status | Evidence |
|---|---|---|---|
| 28 | Stage-4 accepts exports from stages 1, 2 or 3; the new operations (replan, series-amend) work on an imported series, including an occurrence already cancelled before export | verified | `TestImportAcceptsStage3ShapedExportAndSupportsReplanAndAmend` (hand-built stage-3-shaped export: no `closures` field, no `plans` map, a series with one already-cancelled occurrence; both `/series/.../amend` and `/restaurants/.../replans` run successfully against the imported state); stage-1/stage-2 compatibility inherited unchanged and re-verified by the carried-forward `TestImportAcceptsStage1ShapedExport` (now also adopts into a series), `TestImportAcceptsRealStage1Export` (real stage-1 binary subprocess), `TestImportAcceptsStage2ShapedExport` |

## Demo

| # | Statement | Status | Evidence |
|---|---|---|---|
| 29 | `demo/` widened: a future evening at Harbour Table has a genuinely feasible plan that moves ≥1 booking when one table closes | verified | the existing demo fixture already provides this (the diner's Window 1 booking, tomorrow evening, with Window 2/Booth/Long table all free at that time) — no new bookings were needed; confirmed by running `load_demo.py` against a live built container and executing the exact `DEMO.md` replan walkthrough: preview returned `moved_count:1`, the Window 1 reference reassigned to Window 2 with `unused_seats:0`; apply then succeeded (201), the booking's history shows a `reassigned` entry naming the plan, and a repeat `GET /availability` no longer offers Window 1 for that slot |
| 30 | `DEMO.md` gives the exact table and closure start/end to enter, what the preview should show, and the manager login to apply it | verified | file inspection; every command in the new "Seating changes (replans)" section was run manually end-to-end during this verification pass, including the `python3 -c` snippet that reads the booking's own offset-aware `starts_at`/`ends_at` (dates are computed relative to run day, so they can't be hardcoded) |

## Verification commands run this stage

```
docker build -t tablekeeper-stage4 .
docker run --rm -v "<repo>:/src" -w /src/stage-4 golang:1.23-alpine go vet ./...
docker run --rm -v "<repo>:/src" -w /src/stage-4 golang:1.23-alpine go test ./...
docker run --rm -v "<repo>:/src" -w /src/stage-4 golang:1.23 sh -c "CGO_ENABLED=1 go test -race ./... -timeout 600s"
python demo/test_load_demo.py
docker run -d --name tk4test -e PORT=8080 -p 18199:8080 tablekeeper-stage4
python demo/load_demo.py http://localhost:18199
# followed DEMO.md's "Seating changes (replans)" section literally end-to-end: preview, apply,
# history, re-check availability -- all matched what DEMO.md documents
docker rm -f tk4test
python -m harness run --track tablekeeper --repo <repo> --stage 4 --mode host --out <out>
python -m harness check <repo> --track tablekeeper
```

## Judgement calls

- **Restaurant revision union rule**: stage 3's spec text named only series adoption and a
  changed moves-batch as triggers (the field didn't exist yet, so nothing else needed to bump
  it for any externally-visible purpose); stage 4's spec text restates "new booking, real
  amendment, cancellation, policy publication, plan application" without repeating stage 3's
  two triggers. Per "extends all earlier stages... all earlier requirements apply," I took the
  union of both lists (new booking, real amendment, cancellation, policy publication, series
  adoption, changed moves-batch, plan application) and added the four stage-3-dispatch-era
  triggers that weren't previously wired to the counter (`rest.Revision++` now in
  `handleCreateReservation`, `handleCancelReservation`, `handleAmendReservation`'s real-change
  path, and `handlePublishPolicy`). `TestRestaurantRevisionIncrementsOnEveryTrigger` exercises
  all four of the newly-added triggers explicitly.
- **Optimizer implementation**: exhaustive backtracking search over every candidate
  single/pair option per considered booking, bounded by the spec's own hard limits (≤6 tables,
  ≤4 pairs, ≤6 bookings ⇒ at most 10 options per booking, 10^6 worst-case leaves) rather than a
  greedy heuristic, specifically because the spec explicitly warns that a wrong tie-break can
  look correct on simple fixtures while being wrong in general. Performance was not a concern
  at this bound (all tests complete in well under a second); correctness against the exact
  three-criterion lexicographic order was the only design goal.
- **Apply's re-validation is the restaurant-revision check alone, not a fresh occupancy
  re-scan**: justified because every operation that could invalidate a previewed plan's
  feasibility already bumps `restaurant_revision` (see judgement call above) — if the revision
  preview saw still matches, nothing relevant changed, so replaying the stored assignments is
  safe. This is also why `apply` never re-runs `solveReplan`.
- **Demo**: no new bookings were added to `demo/fixture.json` or `load_demo.py`'s generated
  reservations, since the existing Window 1 booking already provides a clean, unambiguous
  feasible-plan scenario (exactly one alternative table with zero unused seats) without
  touching data other parts of the demo path depend on. Confirmed this by actually running the
  full demo load and replan walkthrough against a live container rather than reasoning about it
  statically.
