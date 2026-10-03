# Stage 1 plan — Tablekeeper API

Starting commit: `c558455` (root commit: `.gitignore` + `mandates/`, no earlier stage folder exists).
Repository: `C:\Users\vivek\df-tablekeeper`. Deliverable folder: `C:\Users\vivek\df-tablekeeper\stage-1\`.

## What this stage is
HTTP API only, no screens. Per the dispatch, `@df-frontend` has no work this stage beyond
being present in the room — there is nothing for it to confirm yet since no earlier stage
exists. Input folder `C:\Users\vivek\df-inputs` (brand/illustrations/reference/VISUAL-BRIEF.md/
NOTICE.md) and its visual brief are **acceptance criteria for a later stage with screens, not
this one** — recorded here so the implementer does not spend effort on them yet, and so the
coordinator does not forget to carry them forward into Stage 2's dispatch.

## Judgement calls (recorded up front)
1. **Stack choice**: left to the implementer to pick a language/framework the whole band can
   build and test quickly, per §2 of the spec ("Language, framework and storage are
   unrestricted"). Implementer records its choice and reasoning in its handoff.
2. **Starting commit**: repo had zero commits; coordinator made the root commit of `.gitignore`
   + `mandates/` under its own identity (`c558455`) so there is a concrete starting point to
   diff against, per "Record the starting commit in your plan."
3. **Conflicts between dispatch message and official spec**: the dispatch instructs "where this
   message and the official spec disagree ... the official spec wins." No disagreement was
   found between the dispatch's pasted spec text and the dispatch's prose — the dispatch pastes
   the spec verbatim. The DST regression-test requirement (Melbourne 2026/2027 transitions) is
   an addition from "where earlier attempts went wrong," not a contradiction, so it is carried
   as a hard requirement.
4. **Idempotency storage semantics**: the spec requires idempotency keys scoped per-user, exact
   method+path+body matching, and atomic single-winner semantics under concurrency. Left to the
   implementer's storage/locking design, to be documented and tested with a concurrent-request
   test per the spec's explicit instruction ("Prove with a concurrent test... Do not rely on
   reading the code").

## Requirements traced to owner (implementer — the only working seat this stage)

| Spec section | Rule | Owner | Verification |
|---|---|---|---|
| §1 | No two confirmed reservations overlap on the same table, incl. under concurrency; half-open interval occupancy | implementer | concurrent-booking test, unit tests on interval math |
| §1 | Retries/rejected requests never create duplicate/partial bookings | implementer | idempotency tests, failure-path tests |
| §2 | Dockerfile + RUN.md, builds/starts with no manual setup, no Python required unless chosen | implementer | `docker build`, `docker run --network none -e PORT=8080 -p 8080:8080 <image>`, then curl /health |
| §2 | Resource limits: 2 vCPU/2GiB, health within 60s, 50 concurrent in flight, 5s/10s timeouts, no outbound net at runtime, image self-contained | implementer | manual timing of container start; load test with concurrent requests |
| §3.1 | Listen on 0.0.0.0, `PORT` env, default 8080 | implementer | run with/without PORT set |
| §3.2 | `GET /health` → 200 `{"status":"ok"}` once ready, within 60s | implementer | curl right after container start |
| §3.3 | `POST /_test/reset` replaces all state from fixture, 204, repeatable, no auth | implementer | reset then query endpoints |
| §3.4 | JSON content type, RFC3339 offsets, unknown fields/params ignored, opaque IDs ≤64 chars | implementer | inspect responses; send extra fields/params |
| §4 | Fixture model: restaurants, tables, opening_hours, reservations; weekday closed = no entry | implementer | reset with varied fixtures incl. a closed weekday |
| §5 | Error envelope `{"error":{"code","message"}}`, exact status/code table, query-param integer strictness (`1e9`,`4.0`,`+4` rejected) | implementer | tests per error case |
| §6 | Signup/login, bcrypt/scrypt/Argon2 hashing, token auth, public vs. authenticated endpoints, email format & password length validation | implementer | signup/login tests, inspect stored password representation (not plaintext) |
| §7 | Idempotency: per-user scope, replay vs. reuse-with-different-body vs. failed-then-reused, concurrent identical requests → exactly one 201 | implementer | sequential + concurrent idempotency tests |
| §8 `GET /restaurants`, `/restaurants/{id}`, `/availability` | Public, no auth; availability slot generation, grid, available_table_ids ordering, closed day → `[]` | implementer | availability tests across weekdays |
| §8 `POST /reservations` | Idempotency-key required, full validation table (table_unavailable, not_on_slot_grid, outside_opening_hours, party_exceeds_capacity, validation_failed, invalid_local_time, not_found), response shape incl. `reference` format | implementer | one test per listed error case |
| §8 `GET /reservations`, `GET /reservations/{reference}` | Caller-scoped list descending by starts_at; 404 (not 403) for others' reservations | implementer | ownership-isolation tests |
| §8 `POST /reservations/{reference}/cancel` | Idempotent-cancel (200 twice), cutoff → 409, frees slot immediately | implementer | cancel-then-check-availability test |
| §8 `PATCH /reservations/{reference}` | Partial update of table/time/party_size, same validation as create, cutoff on **current** start, cancelled → 409 `reservation_cancelled`, atomic release+reserve, id/reference stable | implementer | amendment tests incl. failure leaves original untouched |
| §9 | DST: spring-forward skipped local times → `invalid_local_time` / excluded from availability; fall-back repeated hour resolves to first occurrence only; duration is absolute time not wall-clock | implementer | regression tests for Berlin 2026-03-29/2026-10-25, New_York 2026-03-08/2026-11-01, **plus the earlier-attempt requirement**: Berlin & New_York 2026 **and** 2027 transitions, Melbourne 2026-10-04 (forward) & 2027-04-04 (back), each for day-before/day-of/day-after, covering slot generation, booking and amendment |
| §10 | `GET /_test/export` / `POST /_test/import`, atomic snapshot/replace, preserves tokens/hashes/reservations/idempotency records, reset still clears imported state | implementer | export → mutate → import → verify old receipts/tokens/idempotency replays still valid |
| §11 | `POST /reservation-moves`: atomic multi-booking move, ordering of error precedence, replay semantics, export/import preserves batch receipts | implementer | per-rule tests incl. partial-failure-leaves-nothing-changed, replay test |
| Earlier-attempts note | Opening hours: every weekday incl. one closed day, earliest & latest valid start | implementer | availability tests at open/close boundaries |
| Earlier-attempts note | Concurrency: two simultaneous bookings for the same table/overlapping time, at most one wins, proven not asserted | implementer | concurrent HTTP requests in a test, not a code read |
| Earlier-attempts note | Full spec coverage beyond the supplied harness fixtures: idempotency replays, amendments, atomic moves, export/import | implementer | implementer's own test suite, separate from the harness |
| Stage 2 prep | Keep static file serving / config clean so Stage 2 can add screens without restructuring | implementer | implementer notes the serving approach in its handoff for the next stage |
| First-run experience | No-data screen requirement applies from the first stage **with a user-facing surface** — stage 1 has none, so no screen is owed yet. A demo folder is still useful groundwork but not mandatory this stage since there is no UI to demo. | implementer (optional) | implementer may note whether it deferred this to Stage 2 |

## Checklist ownership
Implementer keeps `stage-1/CHECKLIST.md` per its mandate: one line per normative statement
above, marked verified/failing/unverified with evidence.

## Roles this stage
1. `@df-implementer` builds the whole service in `stage-1/`, commits under its own git identity,
   reports the full revision hash and its own test results.
2. `@df-reviewer` verifies independently from a clean build (fresh `git clone`/checkout of the
   reported revision), runs the harness per the dispatch's Verification section, and checks the
   full spec — not just the example checks — before accepting or sending findings back.
3. `@df-frontend` — no work this stage; present in the room only.

## Verification commands (run by implementer before handoff, re-run independently by reviewer)
```
$env:PYTHONUTF8 = "1"
python -m harness run --track tablekeeper --repo C:\Users\vivek\df-tablekeeper --stage 1 --mode host --out C:\Users\vivek\df-checks\stage-1-attempt-N
python -m harness check C:\Users\vivek\df-tablekeeper --track tablekeeper
```
Each run needs a fresh `--out` directory name (`stage-1-attempt-1`, `-attempt-2`, ...). A green
harness run is evidence, not proof of a finished stage — the spec is the target.
