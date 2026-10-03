# Stage 4 plan — seating changes and recurring amendments (final stage)

Starting commit: `31fb0ef` (Stage 3, accepted). Repository: `C:\Users\vivek\df-tablekeeper`.
Deliverable folder: `C:\Users\vivek\df-tablekeeper\stage-4\` (new copy of `stage-3/`, widened —
`stage-1/`, `stage-2/`, `stage-3/` are never edited again).

Input folder: `C:\Users\vivek\df-inputs` (unchanged since stage 2) — same assets, same
`VISUAL-BRIEF.md`, same `reference/` screenshots.

## Judgement calls (recorded up front)
1. **No conflict found** between the dispatch's pasted spec-4 text and the official spec —
   the dispatch states it pastes `stage-4.md` byte-exact; nothing to arbitrate.
2. **Service recovery page is a dispatch-level product requirement, not a spec requirement.**
   The spec text says explicitly "No new screens are required" for seating changes. The
   dispatch's "What the product must be" section then *adds* a Service recovery page as a
   product decision on top of the spec, built only on the stage-4 API and only after the
   required behavioural work (replans/apply, series amend, screen updates reflecting an applied
   plan) is committed and reported. If building it would put any required behaviour at risk, the
   dispatch says to drop it and say so — treated as a hard priority order: required work always
   wins over this optional screen.
3. **Top bar visibility vs. permission**: per the dispatch and the unchanged `VISUAL-BRIEF.md`
   (its "Managers and Service recovery" section, written back in stage 2), "Service recovery" is
   shown in the top bar to **every signed-in user**, not just managers — the server alone decides
   who may actually use it (403/a friendly refusal), and the page never guesses or offers a role
   switch. This was already anticipated in the brief; stage 4 is simply the stage where it stops
   being absent.
4. **Restaurant-revision counter now has a named response field.** Stage 3 established an
   internal restaurant-revision counter but the spec never named a field to expose it in, so the
   implementer correctly kept it internal-only. Stage 4's spec *does* name a field —
   `restaurant_revision`, in the `replans`/`replans/.../apply` responses — so it must now be
   surfaced there. Its increment rule must be the **union** of stage 3's rule (new booking, real
   amendment, cancellation, policy publication, series adoption, a changed reservation-moves
   batch) and stage 4's restated rule (new booking, real amendment, cancellation, policy
   publication, plan application) — stage 4's sentence is not exhaustive on its own; "extends all
   earlier stages... all earlier requirements apply" carries stage 3's triggers forward too.
5. **Demo widening** is explicitly assigned to `@df-implementer` by the dispatch, consistent with
   stages 2-3.
6. **Concurrency proofs** continue to be required with real concurrent-HTTP tests, not code
   reads, for: plan application atomicity ("concurrent applications must not leave partially
   moved bookings") and series-amendment races ("concurrent amendments from the same expected
   revision may not both make a real change") — same standing bar since stage 1.
7. **"No booking may disappear or be cancelled"** during a replan is a hard constraint on the
   optimizer: every considered booking must receive *some* feasible assignment, or the whole plan
   fails `409 no_feasible_plan` — a plan can never resolve a conflict by dropping a booking.
8. **`explain=true`'s `no_overlap` must treat an applied closure like a conflicting booking** for
   the closed table during the closure window — ties stage-4's seating changes into stage-3's
   explanation machinery; easy to miss since it's one sentence buried near the end of the
   "Seating changes" section.
9. **Order of work**: implementer (replans/apply, series amend, migration, demo) → frontend
   (confirm existing screens reflect an applied plan, *then* build Service recovery) → reviewer.

## Requirements traced to owner

### `@df-implementer` — replans/apply, series amendment, migration, demo
| Spec rule | Verification |
|---|---|
| Copy `stage-3/` → `stage-4/`, widen; earlier stages untouched | diff shows all three unchanged |
| `POST /restaurants/{id}/replans`: manager+idempotency key; `from<to` with explicit offsets else 422 `validation_failed`; unknown table → 404; closure is half-open `[from,to)`; considers every confirmed booking at the restaurant overlapping it; up to 6 tables/4 pairs/6 considered bookings, larger → 422 `planning_limit` | validation + limit tests |
| Each considered booking retains reference/owner/party/start/end/accepted-terms; assigned a single or declared pair with enough capacity **under its own accepted terms**, no conflict with fixed bookings/other assignments/previously-applied closures/the proposed closure; diner cutoffs do **not** block an operator repair; **no booking may disappear or be cancelled** — infeasible → whole plan 409 `no_feasible_plan`, nothing changed | one test per constraint, an infeasible-plan test |
| Optimization order: (1) minimize bookings whose table set changes, (2) minimize total unused seats, (3) minimize the option-rank vector in ascending reference order (singles ranked first in fixture order, then pairs in declared order, from 0) | a test with multiple feasible plans asserting the optimizer picks the spec's exact minimum, not merely *a* feasible plan |
| 201 response shape incl. `plan_id`, `restaurant_revision`, `closure`, `assignments` (every considered booking, reference order, `changed` flag), `moved_count`, `unused_seats` | shape tests |
| `restaurant_revision`: starts at 0 after reset; +1 per successful new booking/real amendment/cancellation/policy publication/series adoption/changed moves-batch/plan application (union of stage-3's and stage-4's increment rules — see judgement call 4); no-op writes, failures, **previews**, and replays never increment it | increment tests covering every trigger, explicit preview-does-not-increment test |
| Preview stores only the plan — no closure/occupancy/reservation-revision/history change | state-untouched-by-preview test |
| `POST /restaurants/{id}/replans/{plan_id}/apply`: body `{}`, manager+idempotency key; 201 with `plan_id`/`restaurant_revision`/`reservations` (every considered booking, reference order); unknown plan or another restaurant's plan → 404; an intervening restaurant revision → 409 `stale_plan`, nothing changed; already applied under a different key → 409 `plan_already_applied`; replay of the successful key → original response, 200; atomic | per-rule tests |
| Application: records closure+all assignments together; each **moved** booking gets exactly one revision bump + one `reassigned` history entry (with a `table_ids` change and `plan_id`); accepted terms/times stay identical; **unmoved bookings gain nothing**; restaurant revision bumps **once for the whole plan** | per-field tests |
| After application, the closure excludes its singles/pairs from availability and rejects creates/amendments with 409 `table_unavailable` for the closure window; `explain=true`'s `no_overlap` is false for a closure exactly as for a conflicting booking | closure-exclusion test, explain-reflects-closure test |
| **Concurrent applications must not leave partially-moved bookings** — real concurrent-HTTP proof, not a code read; a closure at another restaurant does **not** invalidate this plan | concurrency test, cross-restaurant-independence test |
| `POST /series/{series_id}/amend`: owner-only idempotent write, 404 unknown/other-owner, 401 no token; body validation (`expected_revision` positive int, `from_index` 0..count-1, `local_time` exact `HH:MM` 00:00..23:59, booleans invalid) → 422; revision mismatch → 409 `stale_revision` **before** any occurrence's cutoff/validation | per-field validation tests |
| Consider indices ≥ `from_index`, **excluding cancelled and exception-marked occurrences**; change their clock time on their **original scheduled local dates**, keeping reference/owner/party/current tables; identical-result = no-op, retains terms; each real change checks its old accepted cutoff then adopts the resulting date's policy (same as an individual `PATCH`) | per-occurrence-state tests |
| Resulting occurrences must not conflict with unchanged occurrences/other bookings/applied closures; on failure nothing changes (histories/idempotency/revisions untouched); non-occupancy errors take precedence in occurrence-index order, else an occupancy conflict → `table_unavailable` | conflict + precedence-ordering tests |
| Success: 201 current series response; each changed occurrence gets one changed-history-entry + one reservation-revision; series **and** restaurant revisions each +1 once for the whole operation, only if anything actually changed; **series amendments never mark exceptions**; all-no-op or an empty eligible set succeeds with **no** revision changes; replay returns the original response, 200, even after further edits/cancellations | per-rule tests incl. all-no-op test |
| Seating repairs (replans) that move series occurrences preserve exception flags/scheduled dates/identities/accepted terms; each affected series revision +1 **per plan application**, if ≥1 member moved | replan-touches-series test |
| **Concurrent amendments from the same `expected_revision` may not both make a real change** — real concurrent-HTTP proof | concurrency test |
| Stage-4 service accepts exports from stages 1, 2, **or** 3; these new operations (replan, series-amend) must work on an imported series including **already-moved and already-cancelled occurrences** | migration tests, one per source stage, incl. a replan/amend exercised on imported series state |
| `stage-4/demo/`: add bookings so a future evening at Harbour Table has a **feasible** plan that moves ≥1 booking when one table closes; `DEMO.md` gives the exact table/closure window, what the preview should show, and the manager login to apply it | run the loader, follow `DEMO.md`'s new steps literally, confirm the preview and apply match what it documents |

### `@df-frontend` — reflect applied plans first, then build Service recovery
| Spec / dispatch rule | Verification |
|---|---|
| Every earlier screen, route and `data-testid` hook stays intact on the stage-4 service | re-render at 375/768/1280, both themes; re-run the full stage-2/3 behaviour suite |
| Existing availability, confirmation and lookup screens **reflect an applied plan** (a booking moved by a replan shows its new table on `/lookup`; a closed table/window no longer appears available on `/`) — this is required, not optional, and comes **before** the Service recovery work | apply a real plan via the API, then confirm the existing screens show the result correctly |
| **Only after** the above is committed and reported: build the Service recovery page — top bar item visible to every signed-in user (not gated client-side; the server decides and the page shows a friendly refusal for a non-manager, "This is for restaurant managers," with no role switch); a form (restaurant, table, closure start/end as the restaurant's **local** time, shown with its timezone, converted via the restaurant's own zone rules, not the browser's); "Preview plan" → readable table (reference, party size, start time, tables before/after, changed/unchanged) + moved count + unused seats, then "Apply plan"; distinct plain-word states: loading, previewed, applied (with updated bookings), refused-non-manager, no-feasible-plan, stale-plan (offers a new preview), planning-limit, error | render the page and every listed state at 3 widths, both themes; walk the actual preview→apply flow against a real plan |
| The Service recovery page must never weaken any required behaviour; if it would, drop it and say so | frontend's own judgement call, recorded either way |
| No raw API field in rendered text (continuing stage 3's constraint — now also `restaurant_revision`, `plan_id` as literal values) | DOM scan |
| `DESIGN.md`/`NOTES.md` updated for this stage's new screen | read files |

## Checklist ownership
Implementer keeps `stage-4/CHECKLIST.md`. Frontend updates `DESIGN.md`/`NOTES.md`, and since this
stage adds a real new screen, renders/scores it per the frontend mandate's full process (passes,
scores, at least one improvement pass).

## Roles this stage (one writer at a time, in this order)
1. `@df-implementer` copies `stage-3/` → `stage-4/`, widens to the above, widens `demo/`, commits,
   reports full revision + test results.
2. `@df-frontend` builds on that commit: confirms/fixes the existing screens reflecting an applied
   plan first, commits and reports that alone if it's a separate logical unit, **then** builds
   Service recovery, renders/scores it, commits, reports.
3. `@df-reviewer` verifies behaviour (incl. stage 1-3 regression) AND rendered screens (incl. the
   new Service recovery page, scored per the mandate) AND the demo path, accepts or sends findings.

## Verification commands
```
$env:PYTHONUTF8 = "1"
python -m harness run --track tablekeeper --repo C:\Users\vivek\df-tablekeeper --stage 4 --mode host --out C:\Users\vivek\df-checks\stage-4-attempt-N
python -m harness check C:\Users\vivek\df-tablekeeper --track tablekeeper
```
Fresh `--out` directory per run. A green harness run is evidence, not proof of a finished stage —
the spec text (and, for anything visual, the brief/reference screenshots) is the target. This is
the final stage: the end-to-end acceptance, once reached, closes the whole dark-factory run.
