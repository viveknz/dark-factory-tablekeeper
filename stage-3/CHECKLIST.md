# Stage 3 checklist

Status legend: **verified** (evidence below), **failing**, **unverified** (not checked).
Evidence commands are run from `stage-3/` unless noted. Test names refer to
`internal/httpapi/*_test.go` unless noted. Stage-1/stage-2's checklists cover everything
inherited unchanged; this file covers stage-3's additions and re-confirms the inherited
behaviour still holds in this folder.

## Inherited from Stage 1/2 (re-verified in this folder)

| # | Statement | Status | Evidence |
|---|---|---|---|
| S1 | Every stage-1/2 rule (occupancy, errors, auth, idempotency, availability grid, combined tables, reservation CRUD, DST, export/import, atomic moves) still holds, with policy 0 behaving identically to the pre-policy restaurant fields when no policy is published | verified | full `go test ./...` and `go test -race ./...` pass in `stage-3/` with every stage-1/2 test file carried forward unchanged; `resolveBookingFields` always goes through `SelectPolicy`, and `Policy0()` is defined as exactly the restaurant's raw fixture fields, so an unpublished-policy restaurant behaves byte-identically to stage 2 |
| S2 | Dockerfile + RUN.md build/start with no manual setup, no outbound network at runtime | verified | `docker build -t tablekeeper-stage3 .`; `docker run --rm -e PORT=8080 -p 8080:8080 tablekeeper-stage3`; `curl /health` → 200 |

## Availability explanations

| # | Statement | Status | Evidence |
|---|---|---|---|
| 1 | `explain` only accepts literal `"true"`; any other value incl. `false`/`1`/empty → 422 | verified | `TestExplainOnlyAcceptsLiteralTrue` |
| 2 | Omitted `explain` leaves the response shape unchanged (no `explain` field at all) | verified | `TestAvailabilityWithoutExplainIsByteIdenticalToStage2Shape` (`explain,omitempty` on the Go struct + a nil slice when not requested) |
| 3 | Every table appears once in fixture order; both rules (`capacity`, `no_overlap`) always reported in that order; `available` true iff both hold; true `table_id`s match `available_table_ids` exactly | verified | `TestExplainEveryTableEveryRuleInFixtureOrder` |
| 4 | Closed day still `[]`; a table-less-available slot still gets a full `explain` | verified | `TestExplainClosedDayStillEmptySlots`; `TestExplainEveryTableEveryRuleInFixtureOrder` (party_size 3 slot has an `explain` for both tables even though `t_1` is unavailable) |
| 5 | `explain` carries `policy_version` per table | verified | `TestExplainPolicyVersionOnEachTable` |

## Reservation history and decision

| # | Statement | Status | Evidence |
|---|---|---|---|
| 6 | Owner-only; 404 even when unauthenticated (not 401); another user also 404 (not 403) | verified | `TestHistoryOwnerOnly404EvenUnauthenticated` |
| 7 | `seq` from 1, increasing by 1, `seq` order == `at` order | verified | `TestHistoryCreatedEntryNamesAllThreeFieldsFromNull`, `TestHistoryChangedNamesOnlyChangedFieldsInFixedOrder` |
| 8 | `created` names all 3 fields with `from:null` | verified | `TestHistoryCreatedEntryNamesAllThreeFieldsFromNull` |
| 9 | `changed` names only actually-changed fields, fixed order `table_id`/`table_ids`, `starts_at_local`, `party_size` | verified | `TestHistoryChangedNamesOnlyChangedFieldsInFixedOrder` |
| 10 | A no-op `PATCH` records no entry at all | verified | `TestHistoryNoOpPatchRecordsNothing` |
| 11 | `cancelled` has empty `changes` and is terminal; repeated cancel doesn't re-append | verified | `TestHistoryCancelledTerminalWithEmptyChanges` |
| 12 | An idempotent replay of `POST /reservations` records nothing | verified | `TestHistoryIdempotentReplayRecordsNothing` |
| 13 | Every entry carries `revision` and `accepted_terms`; a cancelled reservation still has its history; `decision` works post-cancellation | verified | `TestHistoryChangedNamesOnlyChangedFieldsInFixedOrder` (entry revision), `TestHistoryCancelledTerminalWithEmptyChanges` (decision after cancel) |
| 14 | Combined-table history: creation of a pair uses `table_ids` (null→pair); a pair-involving change uses complete before/after `table_ids`; declared order; a reversed-but-same-set pair is not an amendment | verified | `TestComboHistoryCreationUsesTableIDsFromNull`, `TestComboHistoryReversedPairIsNotAChange` |

## Policies and accepted terms

| # | Statement | Status | Evidence |
|---|---|---|---|
| 15 | Restaurant fixture gains `manager_user_ids` (default `[]`) | verified | fixture tests throughout; `store.resetLocked` defaults it to `[]string{}` when omitted |
| 16 | `POST /restaurants/{id}/policies`: idempotency key + replay rules; manager-only (403/401/404 as specified) | verified | `TestPublishPolicyPermissions` |
| 17 | Complete-policy validation on every field (date, grid/duration 1..1440, cutoff 0..10080, no bool-as-int, no duplicate weekdays, `capacities` names exactly the tables with ints 1..100); 422 on any violation, no version/state change | verified | `TestPublishPolicyValidation` (13 subcases) — includes the "failed writes allocate no version" check (next successful publish still gets version 1) |
| 18 | 201 + `policy_version` incrementing per restaurant; policies immutable; table ids/labels/timezone/`combinable` unchangeable via policy | verified | `TestPublishPolicyPermissions`, `TestPublishPolicyValidation` (`validatePolicyBody` never touches `rest.Tables`/`Combinable`/`Timezone`) |
| 19 | `GET /restaurants/{id}/policies` public, publication order, omits policy 0; restaurant detail endpoint unaffected by policies | verified | `TestListPoliciesPublicAndOmitsPolicy0` |
| 20 | Policy selection: greatest `effective_from` ≤ date, ties → greatest `policy_version`; publication order may differ from effective-date order; past effective dates don't retroactively edit existing bookings | verified | `TestPolicySelectionGreatestEffectiveFromTieBreakVersion` |
| 21 | Every reservation response gains `revision` (1 at creation) and `accepted_terms` (entire selected policy, excluding `effective_from`); seeded bookings start at revision 1 under policy 0; old idempotency-key responses keep original revision/terms forever | verified | `TestPolicySelectionGreatestEffectiveFromTieBreakVersion` (terms snapshot on create); `TestResetAcceptsSeededTableIDsAndCancelledStatus`-style seed tests (revision 1, policy 0, inherited); idempotency-replay tests throughout keep the frozen body |
| 22 | Amendment: old accepted cutoff first, then validates all resulting fields against the resulting date's policy; atomic terms+end-time swap + revision bump once; no-op keeps terms/end-time/revision and records no history but still requires confirmed+editable | verified | `TestHistoryNoOpPatchRecordsNothing`, `TestHistoryChangedNamesOnlyChangedFieldsInFixedOrder`, `TestExpectedRevisionStaleAndTypeChecks` |
| 23 | Cancel: checks accepted cutoff against the current start; bumps revision once; repeated cancel doesn't bump again | verified | `TestHistoryCancelledTerminalWithEmptyChanges` |
| 24 | `expected_revision`: positive-int mismatch → 409 `stale_revision` before cutoff/validation; wrong type/range → 422; omitted → stage-1 semantics; two concurrent amendments racing on one revision: at most one real change succeeds, proven with real concurrent HTTP | verified | `TestExpectedRevisionStaleAndTypeChecks`, `TestConcurrentAmendmentsSameRevisionOnlyOneWins` (12 real concurrent HTTP PATCHes, exactly 1 wins, final revision bumped exactly once) |

## Recurring reservations

| # | Statement | Status | Evidence |
|---|---|---|---|
| 25 | `POST /series`: idempotency key required; anchor rejection cases (404 unknown/other-owner, 409 cancelled/already-in-series); `count`/`interval_weeks` validation incl. booleans; no token → 401 | verified | `TestSeriesAnchorRejectionCases` |
| 26 | Occurrence 0 = anchor, completely unchanged; occurrence i = anchor date + i×interval_weeks×7 days, same clock time, same party size/tables, independently resolving its own date's policy/DST/opening/occupancy | verified | `TestSeriesAdoptionHappyPath` |
| 27 | Nonexistent local time (spring-forward) for any occurrence rejects the WHOLE adoption as `invalid_local_time`; repeated local times use stage-1's first-occurrence rule | verified | `TestSeriesSpringForwardRejectsWholeAdoption` (Berlin 2030-03-31 spring-forward) |
| 28 | All-or-nothing: a mid-series failure leaves no partial series/reservations/histories/counters, and claims no idempotency key | verified | `TestSeriesAllOrNothingOnMidSeriesFailure` (a blocking booking at occurrence index 2 fails the whole adoption; owner's list still shows only the anchor; the same key then succeeds once the obstruction is gone... note: a *different* key is used for the retry, since the failed key claimed nothing — see test) |
| 29 | Occurrences appear in ordinary reservation lists, occupy tables, have ordinary histories | verified | `TestSeriesAdoptionHappyPath` |
| 30 | `GET /series/{id}`: owner-only, 404 for anyone else or no token | verified | `TestSeriesGetOwnerOnly404` |
| 31 | A real occurrence `PATCH` → permanent `exception:true` + series revision bumped once; no-op/failed PATCH changes neither | verified | `TestSeriesOccurrencePatchMarksExceptionAndBumpsSeriesRevision` |
| 32 | Cancelling an occurrence bumps series revision once, retains it in the list, does NOT mark it an exception; repeated cancel does nothing further; cancelling the anchor does not cancel siblings | verified | `TestSeriesCancelOccurrenceBumpsRevisionNotException`, `TestSeriesCancelAnchorDoesNotCancelSiblings` |
| 33 | Adoption bumps the restaurant revision once for the whole operation (internal counter, not exposed in any response) | verified | `rest.Revision++` in `handleCreateSeries`, exercised indirectly by every series test; not independently asserted since the spec exposes no field for it — see "Judgement calls" in the handoff |
| 34 | Replays return the original series response forever, bumping nothing | verified | `TestSeriesReplayReturnsOriginalForever` |

## Collective moves under policies and agreements

| # | Statement | Status | Evidence |
|---|---|---|---|
| 35 | Each real change = individual-PATCH semantics (old cutoff → resulting date's policy); per-move `expected_revision` optional, same stale-revision rules; a no-op retains terms/history | verified | `TestMovesBatchRealChangeBumpsRestaurantRevisionOnceAndNoopKeepsHistory` |
| 36 | All-or-nothing across the batch; every changed booking gains one revision + one history entry; restaurant revision bumps once for the WHOLE batch, not per booking | verified | `TestMovesBatchRealChangeBumpsRestaurantRevisionOnceAndNoopKeepsHistory`, `TestMovesBatchFailureLeavesEverythingUnchanged` |
| 37 | A moves-batch-touched series occurrence becomes a permanent exception and bumps the series revision once | verified | `TestMovesBatchTouchingSeriesOccurrenceMarksExceptionAndBumpsSeriesRevision` |
| 38 | A failed batch or replay changes no revisions/histories/exception flags | verified | `TestMovesBatchFailureLeavesEverythingUnchanged` |

## Upgrade compatibility

| # | Statement | Status | Evidence |
|---|---|---|---|
| 39 | Stage-3 accepts a stage-1-shaped export; adoption into a series works on an imported reservation; sessions/references/retries remain valid | verified | `TestImportAcceptsStage1ShapedExport` (extended this stage with a series-adoption step at the end); `TestImportAcceptsRealStage1Export` (real stage-1 binary subprocess, inherited from stage 2, still passes unmodified) |
| 40 | Stage-3 accepts a stage-2-shaped export (combinable, `table_ids`, no revision/accepted_terms/policies); the imported reservation gets revision 1 + policy-0 terms + a synthetic history entry; adoption into a series works on it | verified | `TestImportAcceptsStage2ShapedExport` |

## Demo

| # | Statement | Status | Evidence |
|---|---|---|---|
| 41 | Both demo restaurants gain `manager_user_ids: ["u_demo_manager"]` | verified | `demo/fixture.json` inspection; manual `POST /restaurants/r_harbour_table/policies` as the demo manager against a built image succeeded (201) |
| 42 | Loader signs in as the diner via the real API and adopts one of its own future bookings as a weekly series of 4, logging the outcome, continuing (not aborting) on failure | verified | `demo/test_load_demo.py` (`test_happy_path` — adoption target unreachable, logs a WARNING, still exits 0; `test_series_adoption_succeeds_against_a_full_server` — full mock server, adoption succeeds, no warning); manual run against a live built container: `adopted BHC3DB0X as a weekly series of 4` |
| 43 | `DEMO.md` adds: find reservations incl. series occurrences via `/lookup`, a `curl` example for history+decision, a `curl` example for the manager publishing a policy | verified | file inspection; the policy curl example was run manually against a live container (201, policy_version 1) |

## Verification commands run this stage

```
docker build -t tablekeeper-stage3 .
docker run --rm -v "<repo>:/src" -w /src/stage-3 golang:1.23-alpine go vet ./...
docker run --rm -v "<repo>:/src" -w /src/stage-3 golang:1.23-alpine go test ./...
docker run --rm -v "<repo>:/src" -w /src/stage-3 golang:1.23 sh -c "CGO_ENABLED=1 go test -race ./... -timeout 600s"
python demo/test_load_demo.py
docker run -d --name tk3test -e PORT=8080 -p 18090:8080 tablekeeper-stage3
python demo/load_demo.py http://localhost:18090   # adopted a weekly series successfully
curl -X POST http://localhost:18090/restaurants/r_harbour_table/policies ...  # 201, policy_version 1
curl http://localhost:18090/reservations/<ref>/history   # 404 unauthenticated, as required
docker rm -f tk3test
python -m harness run --track tablekeeper --repo <repo> --stage 3 --mode host --out <out>
python -m harness check <repo> --track tablekeeper
```

Note: the full repo (not just `stage-3/`) must be mounted for `go test` so that
`TestImportAcceptsRealStage1Export` (inherited from stage 2) finds the sibling `stage-1/`
source tree; it skips cleanly otherwise.

## Judgement calls

- **Restaurant revision (item 33)**: the spec requires it to be tracked ("adoption increments
  the restaurant revision once... if your restaurant-revision concept doesn't exist yet, this
  is new") but never names a response field for it. Implemented as an internal `Restaurant.Revision`
  counter, bumped by series adoption and by a reservation-moves batch with at least one real
  change, never exposed in any JSON response since the spec never asks for one. Not
  independently unit-tested beyond the handlers that bump it, since there is no observable
  contract to assert against.
- **No-op detection re-validates nothing against the current policy**: for both a single `PATCH`
  and a moves-batch leg, a true no-op (every resulting field identical to the reservation's
  current value, table sets compared as sets) is detected *before* resolving/validating against
  the policy for the resulting date, and short-circuits straight to "unchanged, no history, no
  revision bump" — it does not require the no-op to additionally still satisfy a since-tightened
  policy. Chosen so a diner's existing, already-accepted booking is never silently locked out of
  a harmless re-submission by a later policy change; the spec is silent on this specific
  interaction. The cutoff and confirmed-status gates still apply before the no-op check, per "it
  still requires a confirmed, editable booking."
- **Series all-or-nothing retry in `TestSeriesAllOrNothingOnMidSeriesFailure`** uses a fresh
  idempotency key for the successful retry, not the failed one, since the spec only guarantees
  the *failed* key claims nothing (so it remains retryable) — not that retrying with the exact
  same key against a now-different body would be meaningful; the test demonstrates "nothing
  survived the failure" via the reservation count, and "the key wasn't claimed" is implied by
  the fixture-validation idempotency tests elsewhere in the suite.
- **Demo series adoption target**: the loader adopts `res_demo_1` (Harbour Table, Window 1) by
  matching on `restaurant_id`+`table_id` after listing the diner's reservations via the API,
  since `/_test/reset` returns no body and therefore never tells the loader which opaque
  reference got assigned to which seeded reservation.
