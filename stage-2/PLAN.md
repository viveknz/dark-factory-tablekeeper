# Stage 2 plan — online booking and combined tables

Starting commit: `085bb36` (Stage 1, accepted). Repository: `C:\Users\vivek\df-tablekeeper`.
Deliverable folder: `C:\Users\vivek\df-tablekeeper\stage-2\` (new copy of `stage-1/`, widened —
`stage-1/` itself is never edited again).

Input folder: `C:\Users\vivek\df-inputs` (read-only) —
`brand/tablekeeper-icon.svg`, `illustrations/undraw_booking_8vl5.svg`,
`illustrations/undraw_confirmation_31jc.svg`, `illustrations/undraw_eating-together_mr7m.svg`,
`illustrations/undraw_login_weas.svg`, `illustrations/undraw_no-data_ig65.svg`, `NOTICE.md`,
`reference/home-signed-in-{dark,light}.png`, `reference/home-signed-out-{dark,light}.png`,
`reference/sign-in-{dark,light}.png`, `reference/REFERENCE.md`, `VISUAL-BRIEF.md`. These and the
visual brief are acceptance criteria with the same weight as the spec's behavioural rules, and
apply in full starting this stage (stage 1 had no screens).

## Judgement calls (recorded up front)
1. **No conflict found** between the dispatch's pasted spec-2 text and the official spec — the
   dispatch states it pastes `stage-2.md` byte-exact; nothing to arbitrate.
2. **Demo ownership**: the dispatch explicitly assigns `stage-2/demo/` to `@df-implementer` (step
   1 of "Roles this stage"), overriding the frontend mandate's more general "the band builds this"
   language — not a conflict, just the dispatch being specific about who on the band does it.
3. **"Competing clients and uncertain outcomes" (spec §"Competing clients...")** is primarily
   browser-side logic (stale-response discarding, retry-with-same-idempotency-key on lost
   responses, `booking-error`/`booking-uncertain` display) — owned by `@df-frontend`, built on the
   API contract stage-1 already provides (idempotency keys, `409 table_unavailable`). No new API
   endpoint is implied; if frontend finds an API gap, it reports to coordinator to route to
   implementer rather than touching `internal/httpapi` itself.
4. **"Existing clients after an upgrade"** (stage-1 export accepted by stage-2 service, sessions
   and idempotency records survive) is a backend/data-model guarantee — owned by
   `@df-implementer`. The frontend's existing retry/lookup logic should cope automatically since
   it only ever talks to the current server state; no frontend-specific work is implied beyond not
   breaking that retry logic.
5. **First-run no-data screen**: copy and illustration are `@df-frontend`'s job (per its mandate's
   "no-data screen explains the product... points to demo/DEMO.md"); the demo folder and
   `DEMO.md` content it points to are `@df-implementer`'s job. Sequenced so frontend builds this
   screen only after the implementer's `demo/DEMO.md` exists to reference by name.
6. **Order of work**: implementer (API + combined tables + demo) → frontend (screens, on top of
   the implementer's committed revision) → reviewer, matching the dispatch's explicit "Roles this
   stage" ordering and the general one-writer-at-a-time rule.

## Requirements traced to owner

### `@df-implementer` — API, data model, demo, static serving
| Spec rule | Verification |
|---|---|
| Copy `stage-1/` → `stage-2/`, delete any nested VCS dir, widen; stage-1 untouched | diff shows stage-1/ unchanged; stage-2/ builds and starts standalone |
| Restaurant fixture gains `combinable: [[t_a,t_b],...]` — pairs only, not transitive | fixture round-trip test; reject 3+ entries structurally (field doesn't support it) |
| `GET /availability` slots gain `available_options` (singles then pairs, fixture/`combinable` order, pair `table_ids` in `combinable` order); `available_table_ids` unchanged | availability tests incl. a combinable pair and a non-combinable same-capacity pair |
| `POST /reservations` / `PATCH` accept `table_ids` (and still `table_id` as a 1-element set); both present → 422 `validation_failed`; response always has `table_ids`, plus `table_id` iff exactly one member | combination booking tests, both-fields-sent test |
| Combination errors: pair not in `combinable` → 422 `combination_not_allowed`; >2 tables → 422 `combination_not_allowed`; any member table taken → 409 `table_unavailable`; party_size > summed capacity → 422 `party_exceeds_capacity`; duplicate table id → 422 `validation_failed` | one test per case |
| Cancel frees every table in the set | cancel-then-availability test on a combo booking |
| `POST /reservation-moves` accepts `table_ids` per move; no table in overlapping resulting bookings | moves test with a combo leg |
| Concurrent requests give the same result as some serial order, at every read, including combos | concurrency test booking the same pair from two clients |
| Seeded `reservations` may carry `table_id` or `table_ids`, and a `status` of `cancelled` | fixture tests for both shapes |
| Existing clients after upgrade: a stage-2 service accepts a stage-1 export; sessions/references/idempotency records survive import; retryable lost-response bookings remain retryable after import, mid-request migration not required | export(stage-1-shaped)→import→verify token/reference/replay still valid test |
| Serve the frontend's built static files (HTML/CSS/JS/images) with correct content-types, offline, from this same service | manual fetch of a static asset after frontend commits |
| `stage-2/demo/fixture.json`: exact two-restaurant/table/account fixture the dispatch specifies (Harbour Table + Lantern Noodle Bar, Australia/Melbourne, combinable Window 1+Window 2, diner/manager accounts, `DemoPass-2026!`) | loader test asserts the fixture matches spec'd shape |
| `stage-2/demo/load_demo.py`: stdlib-only, posts fixture + a few future-dated confirmed reservations (computed from run day) to `/_test/reset`, prints logins, logs each step, exits non-zero with a clear message on failure | run it against a live container, inspect output and exit code |
| `stage-2/demo/DEMO.md`: exact build/start/load/open commands, both logins, what to try first, plus a plain `curl` fixture-only alternative for a Python-less machine | a stranger (reviewer) follows it literally |
| Optional `DEMO_SEED=1` env var auto-loads the same demo at startup, off by default, must not change reset's own behavior | start with/without the var, confirm reset still fully replaces state either way |
| A loader test (automated) | implementer's own test suite |

### `@df-frontend` — all four routes, every state, visual system
| Spec / brief rule | Verification |
|---|---|
| Routes `/`, `/signup`, `/login`, `/lookup` return HTML, reachable by URL; other screens via UI | manual navigation render check |
| One-page booking flow on `/`: search card → results grid → sticky review panel → confirmation, no page change/modal/wizard; signing in is the only step that leaves `/` and returns to the chosen slot | render at 375/768/1280, both themes, compare to `reference/home-*.png` |
| All `data-testid` hooks from the spec tables (signup/login, grid, booking form, confirmation, lookup, combination cells `slot-{t_a}+{t_b}-{HH:MM}`, `confirmation-tables`, `reservation-tables`) present with exact semantics (`data-available`, text contents) | DOM inspection per screen/state |
| Competing clients / uncertain outcomes: stale search responses discarded by freshness, not arrival order; `409 table_unavailable` on booking → `booking-error` + refreshed availability + preserved form, no confirmation; lost booking response → nonempty `booking-uncertain`, no `booking-error`/new confirmation, unchanged form retries with the same idempotency key+body, success clears uncertainty and shows the original reference; applies to combination bookings too | scripted network-delay/drop/reorder tests in the frontend's own render/test pass |
| Grid: every returned slot rendered, every cell is text, readable table/combo labels with capacity ("Table 2, up to 4 guests", "Window 1 + Window 2, up to 4 guests"), unavailable ≠ available by more than colour, restaurant timezone + weekly opening hours shown beside the grid | render + accessibility-scan pass |
| Supplied illustrations/brand icon used exactly where the brief places them, copied unchanged into `stage-2/` with `NOTICE.md`; no hand-drawn scenes; icon set has its licence kept alongside it | asset diff against `df-inputs`, visual check |
| Both themes fully designed, default to system preference, remembered; top bar has Find a table / Reservation lookup / signed-in name+Sign out or Sign in+Create account / theme toggle; **no Service recovery item this stage** | render both themes at all 3 widths; grep for an absent "Service recovery" nav item |
| Guided first-run (no-data) screen: explains the product, that restaurants are operator-set-up, and points in plain words to `demo/DEMO.md` | render with an empty `/_test/reset` state |
| `DESIGN.md` written before screen code: identity, visual system (brief's colour/type values), per-screen layout at phone/desktop, reference match per screen | read file, cross-check against what's rendered |
| Quality bar: no horizontal scroll/clipping at 375/768/1280 in both themes, every input labelled, focus always visible, contrast, 24px touch targets / 44px primary, `prefers-reduced-motion` respected | automated a11y scan + manual render pass, ≥2 improvement passes per the frontend mandate, scores ≥4 or documented why not |
| `NOTES.md`: what each pass changed and final scores | read file |

## Checklist ownership
Implementer keeps `stage-2/CHECKLIST.md` (as in stage 1) for every normative API/data rule above.
Frontend keeps `DESIGN.md` and `NOTES.md` per its mandate, covering every screen/state rule above.

## Roles this stage (one writer at a time, in this order)
1. `@df-implementer` copies `stage-1/` → `stage-2/`, widens to the above, builds `demo/`, commits,
   reports full revision + test results.
2. `@df-frontend` builds on that committed revision: `DESIGN.md` first, then all screens/states,
   renders and scores per its mandate, commits, reports full revision + render/score evidence.
3. `@df-reviewer` verifies behaviour AND rendered screens (comparing to `reference/*.png`) AND the
   first-run/demo path as a stranger would, scores design, accepts or sends findings to the owning
   seat (coordinator routes: API findings → implementer, visual/UX findings → frontend).

## Verification commands
```
$env:PYTHONUTF8 = "1"
python -m harness run --track tablekeeper --repo C:\Users\vivek\df-tablekeeper --stage 2 --mode host --out C:\Users\vivek\df-checks\stage-2-attempt-N
python -m harness check C:\Users\vivek\df-tablekeeper --track tablekeeper
```
Fresh `--out` directory per run. A green harness run is evidence, not proof of a finished stage —
both the spec text and the visual brief/reference screenshots are the target.
