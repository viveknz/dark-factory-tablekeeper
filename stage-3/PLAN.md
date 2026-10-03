# Stage 3 plan — booking policies, history and recurring reservations

Starting commit: `640a51c` (Stage 2, accepted). Repository: `C:\Users\vivek\df-tablekeeper`.
Deliverable folder: `C:\Users\vivek\df-tablekeeper\stage-3\` (new copy of `stage-2/`, widened —
`stage-1/` and `stage-2/` are never edited again).

Input folder: `C:\Users\vivek\df-inputs` (unchanged since stage 2) — same assets, same
`VISUAL-BRIEF.md`, same `reference/` screenshots. Still fully in force: no new screens are
required this stage, but every stage-2 visual/behavioural requirement continues to apply to the
screens that already exist.

## Judgement calls (recorded up front)
1. **No conflict found** between the dispatch's pasted spec-3 text and the official spec — the
   dispatch states it pastes `stage-3.md` byte-exact; nothing to arbitrate.
2. **Demo ownership**: `stage-3/demo/` widening is explicitly assigned to `@df-implementer` by
   the dispatch (step 1 of "Roles this stage"), consistent with stage 2's pattern.
3. **Frontend's optional enhancements** (explain-aware grid-cell text, a history view on
   `/lookup`) are explicitly optional in the dispatch, gated on "no required hook or behaviour is
   at risk," and must come **after** the required behavioural work is committed and reported.
   Left to `@df-frontend`'s judgement whether to build them at all; if it does, it records the
   choice and why. No raw API field (e.g. `policy_version`) may ever appear in rendered text,
   per the dispatch — this is a hard constraint even for the optional work.
4. **Revision-based optimistic concurrency** (`expected_revision` on `PATCH`, "two concurrent
   amendments using one revision: at most one real change succeeds") is a new correctness
   requirement in the same family as stage 1's booking-race proof — owned by the implementer,
   who must prove it with a real concurrent-HTTP test, not a code read, per the standing
   "earlier attempts went wrong" bar from stage 1 that still applies to every new concurrency
   claim.
5. **History/decision's 401→404 carve-out**: `GET /reservations/{reference}/history` and
   `GET /reservations/{reference}/decision` return 404 even when the caller is unauthenticated
   — an explicit exception to stage 1's general "no token → 401" rule. Flagged because it is easy
   to miss and a plausible place for a seat to default to the more common 401 behaviour.
6. **Order of work**: implementer (API: policies, history, series, explain, collective moves,
   demo) → frontend (confirm/re-render existing screens, optional enhancements) → reviewer.

## Requirements traced to owner

### `@df-implementer` — policies, explanations, history, series, collective moves, demo
| Spec rule | Verification |
|---|---|
| Copy `stage-2/` → `stage-3/`, widen; `stage-1/`/`stage-2/` untouched | diff shows both unchanged |
| `GET /availability?...&explain=true`: only literal `true` accepted, anything else (incl. `false`,`1`,``) → 422; **omitted → stage-1 shape unchanged**, no `explain` field | tests for valid/invalid `explain` values, and a no-param regression test asserting the old shape is byte-identical |
| `explain`: every table appears once in fixture order, both rules (`capacity`, `no_overlap`) always reported in that order, `available` true iff both hold, and the true-`table_id`s match `available_table_ids` exactly incl. order; closed day still `[]`; a table-less slot still gets a full `explain` | one test per rule combination (capacity fails / overlap fails / both fail / both hold), closed-day test, empty-availability-but-explain-present test |
| `GET /reservations/{reference}/history`: owner-only, **404 even when unauthenticated** (not 401); `seq` from 1 increasing by 1 in `seq`==`at` order; `created` names all 3 fields with `from:null`; `changed` names only actually-changed fields in order `table_id`/`table_ids`, `starts_at_local`, `party_size`; a no-op `PATCH` records nothing; `cancelled` has empty `changes` and is terminal; an idempotent replay records nothing | one test per event type, a no-op-PATCH-records-nothing test, a replay-records-nothing test, an unauthenticated-404 test |
| `GET /reservations/{reference}/decision`: same 404-even-unauthenticated rule, returns `{reference,revision,accepted_terms}`, works post-cancellation | decision tests incl. post-cancel |
| Restaurant fixture gains `manager_user_ids` (default `[]`) | fixture tests |
| `POST /restaurants/{id}/policies`: idempotency key + stage-1 replay rules; manager-only (403 non-manager, 401 no token, 404 unknown restaurant); complete-policy (not patch) validation on every field (`effective_from` real date, grid/duration integers 1..1440, cutoff 0..10080, no bool-as-int, no duplicate weekdays, `capacities` names exactly the restaurant's tables with ints 1..100); 201 + `policy_version` incrementing per restaurant; failed writes/replays allocate no version; table ids/labels/timezone/`combinable` immutable via policy | one test per validation rule, permission tests, version-increment test, failed-write-allocates-no-version test |
| `GET /restaurants/{id}/policies`: public, `{"policies":[...]}` in publication order, **omitting policy 0**; restaurant detail endpoint still shows original fixture config, not policy state | tests for both endpoints staying distinct |
| Policy selection for a booking's local start date: greatest `effective_from` ≤ that date, ties → greatest `policy_version`; publication order may differ from effective-date order; past effective dates don't retroactively edit existing bookings | selection tests incl. out-of-publication-order and a past-effective-date test that doesn't touch old bookings |
| Every reservation response gains `revision` (1 at creation) and `accepted_terms` (snapshot of the *entire* selected policy excluding `effective_from`); seeded bookings start at revision 1 under policy 0; old idempotency-key responses keep their original revision/terms forever | response-shape tests, seeded-booking test, replay-preserves-old-terms test |
| Amendment semantics: checks the **old accepted cutoff** first, then validates **all** resulting fields against the policy for the **resulting** start date; atomically swaps terms+end time and bumps revision **once**; a no-op amendment keeps terms/end-time/revision and records **no** history, but still requires a confirmed+editable booking; a failed amendment changes nothing | amendment tests per each condition |
| Cancel: checks accepted cutoff against the **current** start; bumps revision once; repeated cancel does not bump again | cancel-revision tests |
| `PATCH` optional `expected_revision`: positive-integer mismatch → 409 `stale_revision` **before** cutoff/validation checks; wrong type/range → 422; omitted → stage-1 semantics unchanged; **two concurrent amendments racing on one revision: at most one real change succeeds** — prove with a real concurrent-HTTP test | stale-revision tests, type/range tests, and a genuine concurrency test (two simultaneous PATCHes, same starting revision) |
| Combined-table history: creation of a pair → `table_ids` change (`null`→pair) not `table_id`; a pair-involving change → complete before/after `table_ids` lists; set order is `combinable`'s declared order; a reversed-but-same-set pair is **not** an amendment | combo-history tests incl. the reversed-pair-is-a-no-op case |
| `POST /series`: idempotency key required; anchor must be caller's, confirmed, within its accepted cutoff (404 unknown/other-owner, 409 `reservation_cancelled`, 409 `already_in_series`); `count` 2..12 int, `interval_weeks` 1..4 int, booleans/invalid → 422; occurrence 0 = the anchor unchanged (reference/identity/revision/terms/history/timestamps/original response all untouched); occurrence *i* = anchor's local date + i×interval_weeks×7 days at the same clock time, each independently resolving its own date's policy/DST/opening/occupancy, same party size and table selection as the anchor; a nonexistent local time for any occurrence rejects the **whole** adoption as `invalid_local_time`; repeated local times use stage-1's first-occurrence rule; **all-or-nothing**: no partial series/reservations/histories/counters/idempotency claims survive a failure, and the first failing occurrence in index order determines the error; occurrences appear in ordinary reservation lists/occupy tables/have ordinary histories | one test per anchor-rejection case, an all-occurrences-succeed test, a mid-series-failure-leaves-nothing test, a DST-spanning series test |
| `GET /series/{series_id}`: owner-only (404 for anyone else or no token), current reservation states | ownership isolation test |
| Series mutation: a real individual-occurrence `PATCH` → permanently `exception:true` + series revision bumped once; a no-op/failed PATCH changes neither; cancelling an occurrence bumps series revision once, retains the cancelled occurrence, does **not** mark it an exception; repeated cancel of the same occurrence does nothing further; cancelling the **anchor** does not cancel siblings; adoption itself bumps the restaurant revision once for the whole operation; replays return the original series response forever, bumping nothing | one test per mutation case |
| `POST /reservation-moves` under policies: each real change = individual-PATCH semantics (old cutoff → new policy adoption); per-move `expected_revision` optional, same stale-revision rules; a no-op retains terms/history; all-or-nothing across the batch; each changed booking gains one revision + history entry; restaurant revision bumps **once** for the whole batch; each affected series' revision bumps once and each changed occurrence becomes a permanent exception; a failed batch or replay changes no revisions/histories/exception flags | per-rule tests, incl. a moves-batch-touching-a-series-occurrence test |
| Stage-3 service accepts a **stage-1 or stage-2** export; adoption into a series works on an imported reservation; existing confirmation links/sessions/original retries remain valid | export(stage-1-shaped)→import→adopt-into-series test; export(stage-2-shaped)→import→same test |
| `stage-3/demo/`: restaurants gain `manager_user_ids: ["<manager's id>"]` for both demo restaurants; loader signs in as the diner via the API and adopts one future demo booking as a weekly series of 4, logging the outcome, **continuing (not aborting) on failure**; `DEMO.md` adds: sign in as diner and find reservations via `/lookup`, a curl example for a reservation's history+decision, and a curl example for the manager publishing a policy | run the widened loader against a live container, inspect logs/exit behavior; follow `DEMO.md`'s new curl examples literally |

### `@df-frontend` — keep stage-2 working, optional small enhancements
| Spec / dispatch rule | Verification |
|---|---|
| Every stage-2 screen, route and `data-testid` hook keeps working, visually consistent, on the stage-3 service | re-render at 375/768/1280, both themes; re-run the stage-2 behaviour suite against the stage-3 build |
| No new screens required; the availability grid still follows stage-2 rules | confirm no new route was added unless it's one of the optional enhancements below, and that none is required |
| Top bar still has **no** Service recovery item before stage 4 | grep/visual check for its continued absence |
| No screen shows a raw API field (e.g. `policy_version`) | code/visual review of any new or touched UI text |
| Optional, only after required work lands, only if no required hook/behaviour is put at risk: an `explain=true`-informed reason in an unavailable cell's text; a reservation-history view on `/lookup` | frontend's own judgement call, recorded with reasoning either way |
| `DESIGN.md`/`NOTES.md` updated to reflect this stage (even if the only change is "screens unchanged, re-verified") | read files |

## Checklist ownership
Implementer keeps `stage-3/CHECKLIST.md` for every rule above it owns. Frontend updates
`DESIGN.md`/`NOTES.md` to reflect this stage's (possibly minimal) screen changes.

## Roles this stage (one writer at a time, in this order)
1. `@df-implementer` copies `stage-2/` → `stage-3/`, widens to the above, widens `demo/`, commits,
   reports full revision + test results.
2. `@df-frontend` builds on that committed revision: confirms/re-renders every existing screen and
   state, adds any optional enhancement it judges worthwhile within the stated limits, updates
   `DESIGN.md`/`NOTES.md`, commits, reports.
3. `@df-reviewer` verifies behaviour (including that stages 1 and 2's requirements still hold —
   this is a continuation stage, not a greenfield one) AND the rendered screens AND the demo path,
   scores design if anything visual changed, accepts or sends findings to the owning seat.

## Verification commands
```
$env:PYTHONUTF8 = "1"
python -m harness run --track tablekeeper --repo C:\Users\vivek\df-tablekeeper --stage 3 --mode host --out C:\Users\vivek\df-checks\stage-3-attempt-N
python -m harness check C:\Users\vivek\df-tablekeeper --track tablekeeper
```
Fresh `--out` directory per run. A green harness run is evidence, not proof of a finished stage —
the spec text (and, for anything visual, the brief/reference screenshots) is the target.
