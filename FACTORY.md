# FACTORY.md

How to stand up the factory that built this repository, what it cost, and how it deals with bad work.

## 1. The band

Four seats share one BAND Desktop room, and each seat is its own Claude Code (CLI) session. The mandates live in `mandates/`, one file per seat, named after the seat.

| Seat | Owns | Does not |
|---|---|---|
| df-coordinator | Plans the stage, hands out scoped work, accepts or rejects, reports to the human | Write or review code |
| df-implementer | Services, data, rules, interfaces, the demo data and its loader, the container build | Build screens, accept its own work |
| df-frontend | Every user-facing screen: identity, look, states, accessibility, responsive layout | Change service behaviour, accept its own work |
| df-reviewer | Independent checks of behaviour, rendered screens, design and the first-run experience | Fix code |

The mandates are generic. They describe a factory, not a problem, so the same four files can point at a different spec. They hold no track vocabulary.

**Model and harness.** Claude Code throughout. The mandate headers name `claude-sonnet-5` for the coordinator, implementer and reviewer, and `claude-opus-5` for the frontend. The Claude Code session logs agree: the frontend's session ran only `claude-opus-5`, and the other three ran only `claude-sonnet-5`, apart from a few short start-up sessions. The reviewer's Model field in BAND is set to `claude-sonnet-5` explicitly.

**Runtime settings in BAND.** All four seats show context mode `local_config`, which inherits the host's `~/.claude`, and permission mode `auto`, pinned, in `band status`. I read the rest from the reviewer's settings page: reasoning effort high, no denied tools, MCP servers limited to Band's own. I didn't open those three fields for the other seats. Because the seats inherit the host's Claude Code config, I moved my own `~/.claude` CLAUDE.md and skills aside for this run so they started clean. Anyone reproducing this should do the same.

## 2. Setup

1. Install BAND Desktop and register four Claude Code agents, one per seat.
2. Create a room and add the four agents.
3. Create an empty result repository and give the seats its **absolute path**. A seat works in its own sandbox and can't resolve a relative one. Set a git author.
4. Put the read-only input folder (reference screenshots, approved assets, licences) at an absolute path the seats can read.
5. Dispatch a stage with one message to the coordinator. It holds the full stage spec, the repository path, the previous accepted commit, the `stage-N/` target, the input folder path and the rules below. Paste the spec in full and never paraphrase it.
6. Send nothing else until the coordinator's final report.

I fetch the spec byte-exact from the official repository on the machine that sends the message, so nothing is retyped. A small script assembles each dispatch, which means the stage text is the only thing that changes between stages.

## 3. Design choices and why

**Self-contained handoffs.** A seat only sees messages addressed to it. Every handoff therefore carries the full requirements, the repository path, the commit and the commands, and the mandates forbid "read the room" as a handoff.

**Review is independent and separate from building.** The reviewer re-runs the checks itself, reads the code and never fixes anything. The coordinator accepts only a commit the reviewer actually checked.

**A frontend seat that owns how it looks.** Behavioural checks can all pass and the UI can still be plain, because nobody owns the look and the reviewer only tests behaviour. So the frontend seat owns every screen. The reviewer renders each one at phone and desktop widths in both themes and scores it, and a design finding blocks acceptance the same way a failing test does.

**One writer at a time.** The seats share one repository folder. The coordinator hands over the next item only after the previous owner has committed.

**Supplied visual inputs.** Before the run I prepared a read-only input folder, and the dispatches restate the spec's visual direction. The seats didn't make the assets. The frontend copies them unchanged, and everything under `stage-N/` is still written by the seats. The folder held `VISUAL-BRIEF.md` and six reference screenshots (home signed in and signed out, plus sign-in, each in light and dark) with a layout note. It also held five unDraw illustrations that I downloaded by hand on 3 Oct 2026. They are free for commercial use with no credit required, they can't be used for AI/ML training or redistributed as packs, and I used them unchanged as artwork. The last item was one Tablekeeper icon, original artwork that Claude drew for me from plain geometric shapes, with no third-party source. The folder is not part of this repository.

**One human input per stage.** After the dispatch no seat may ask me anything. Blockers go into the final report, not into a question.

**Stages are folders, not branches.** Each `stage-N/` is the previous accepted stage carried forward and widened, and an accepted folder doesn't change. At the final commit `git diff 31fb0ef HEAD -- stage-1 stage-2 stage-3` is empty, so the stage 3 commit and the end match for those folders. There is one exception earlier in the run. After stage 2's final report the implementer landed `640a51c`, a regression test and a `RUN.md` note in `stage-2/`. The coordinator accepted it and told the band that any further change to an accepted stage needs an explicit reopen.

## 4. What went wrong and how it was handled

| Problem | Handling |
|---|---|
| The `--mode isolated` harness fails on Windows because of a backslash path. It isn't a code defect. | I verified with `--mode host` and told the seats so in every dispatch. |
| The implementer's BAND login expired during stage 2 (06:25 UTC on 3 Oct, `messages` index 990, "OAuth session expired and could not be refreshed"). | I signed in again and restarted that seat's process, not the room. I sent nothing to the room, and the seat carried on. |
| Three more error events in the room log. A coordinator background harness task and a frontend background check both ended as failed in stage 2 (07:08 and 08:14 UTC). An implementer runtime-status notice appeared in stage 4 (13:43 UTC). | None changed code or blocked a stage. I watched them and did nothing. |
| All seats share one Claude subscription and one usage quota. | Not solved, so stages run one at a time. No usage-limit stall shows up in the room log. Its four error events are the ones above, and none concerns limits. |
| Stage 2: the reviewer's two non-blocking findings. An idempotency-replay response lacked `table_ids` on a booking migrated from stage 1, and a paragraph in `demo/DEMO.md` was stale. The coordinator treated both as blocking. | The implementer fixed both in `f76b3f5` and the reviewer re-verified. |
| Stage 2: the coordinator saw that the regression test for finding 1 used a hand-built fixture, not a real stage-1 export. | The implementer's `640a51c` re-tests against a real stage-1 export, and the reviewer confirmed it again. |
| Stage 3: a stale "Stage 2" header and image tag in `demo/DEMO.md` (reviewer, non-blocking). | The implementer fixed it in `31fb0ef` and it was re-verified. |
| After the run ended (the final report came at 14:49 UTC) I restarted the reviewer's process once, at about 15:25 UTC, because I wrongly thought it had stalled. | It reconnected to the same room and session. I sent nothing to the room and changed nothing in the repository. |
| Stages 1 and 4: no reviewer findings, accepted on first review. | No fix round. |
| The frontend found defects in its own screens while the API widened. In stage 3 the screens read static hours instead of the published policy, and unavailable cells always said "already taken". Stage 4 had one more of the same kind. | The frontend fixed them in `d1db9f5` and `934bd61`, and the reviewer reproduced each one independently. |

## 5. How the factory catches bad work

1. The implementer and frontend run the shipped checks and their own checks before they hand off.
2. The reviewer re-runs the checks at the reported commit from a clean build with no network. It tests behaviour the shipped checks never ask about, starts the container with no data like a stranger would, and renders every screen against the references.
3. The coordinator won't accept a commit the reviewer didn't check.
4. I re-run the official harness myself after every stage and don't rely on a seat's own report. Stage 1 gave 120/120, stage 2 gave 25/25, stage 3 gave 7/7 and stage 4 gave 6/6. The highest contiguous stage is 4, at revision `d268821`.

## 6. Time and cost

The times below run from the dispatch message to the coordinator's final report, taken from the room log.

| Stage | Time | Accepted commit |
|---|---|---|
| 1 | 66 min | `085bb36` |
| 2 | 162 min | `f76b3f5` (then `640a51c` after the report) |
| 3 | 123 min | `31fb0ef` |
| 4 | 110 min | `d268821` |

That adds up to about 7 h 40 min.

For size, I counted non-blank lines of Go, JS, CSS and HTML under each `stage-N/`, leaving out tests and image assets. Each folder carries the earlier stages forward, so the numbers don't add up across stages. Stage 1 has 1,724 lines in 16 files, stage 2 has 4,198 in 19, stage 3 has 5,378 in 22 and stage 4 has 6,608 in 24. The Go test files add 1,105, 1,951, 3,277 and 4,181 lines.

**Model spend.** BAND shows $180 on the room header. That is BAND's own estimate at list prices, and I didn't check how it works it out. Its per-seat figures add up to the same: frontend $69.80, implementer $58.62, reviewer $34.59, coordinator $17.41. The frontend is the only Opus seat, so Opus cost about $70 and Sonnet about $111. The four seats' sessions add up to about 510M tokens on BAND's usage page. The seats sign in with a Claude subscription, so I paid no per-token bill.

## 7. Reusing this on another problem

Point the four mandates at a different spec and dispatch as in section 2. Change the repository path, the spec, the input folder and the commands the reviewer runs. The mandates need nothing beyond new seat names and handles.

## 8. Commit trace

Every commit under `stage-N/` traces to a `git commit` tool call in `room.json`. All 17 of them do, and so does the root commit. By seat that is df-coordinator 4, df-frontend 5, df-implementer 8, plus the root commit by df-coordinator. The time is the tool call's `insertedAt`, and the index is its position in the `messages` array, counting from 0.

| Stage | Commit | Seat | `messages` index | UTC time | What it did |
|---|---|---|---|---|---|
| 1 | `d3e65ac` | df-coordinator | 81 | 2026-10-03 04:50:20 | Stage 1 plan: trace every spec rule to the implementer, record judgement calls |
| 1 | `085bb36` | df-implementer | 498 | 2026-10-03 05:33:10 | Stage 1: implement Tablekeeper reservations API (Go, stdlib + bcrypt) |
| 2 | `27e39da` | df-coordinator | 796 | 2026-10-03 06:08:09 | Stage 2 plan: trace spec-2 + visual brief rules to implementer/frontend, record  |
| 2 | `35a75f1` | df-implementer | 1230 | 2026-10-03 06:59:05 | Stage 2: combined-table bookings, stage-1 export compat, and demo data |
| 2 | `5793796` | df-implementer | 1465 | 2026-10-03 07:17:51 | Stage 2: serve the four screen routes without shadowing the API |
| 2 | `31273ce` | df-frontend | 1755 | 2026-10-03 08:06:29 | Stage 2: Tablekeeper screens for /, /signup, /login and /lookup |
| 2 | `f76b3f5` | df-implementer | 2169 | 2026-10-03 08:41:15 | Stage 2: fix review findings - migrated idempotency replay shape, stale DEMO.md |
| 2 | `640a51c` | df-implementer | 2335 | 2026-10-03 09:00:50 | Stage 2: regression-test finding 1 against a real stage-1 export, not a guess |
| 3 | `833c732` | df-coordinator | 2475 | 2026-10-03 09:57:31 | Stage 3 plan: trace policy/history/series/collective-move rules to implementer/f |
| 3 | `f02abec` | df-implementer | 2802 | 2026-10-03 10:57:14 | Stage 3: booking policies, availability explanations, history, recurring series |
| 3 | `d1db9f5` | df-frontend | 2970 | 2026-10-03 11:32:37 | Stage 3: screens follow the published policy, and say why a slot is unavailable |
| 3 | `31fb0ef` | df-implementer | 3300 | 2026-10-03 11:56:30 | Stage 3: fix stale Stage 2 header and image tag in demo/DEMO.md |
| 4 | `cc7bd9f` | df-coordinator | 3418 | 2026-10-03 13:01:04 | Stage 4 plan: trace replan/apply, series amendment, and Service recovery to owne |
| 4 | `5a8dd2b` | df-implementer | 3685 | 2026-10-03 13:42:00 | Stage 4 (final): seating-change plans and recurring-series amendments |
| 4 | `934bd61` | df-frontend | 3785 | 2026-10-03 13:52:44 | Stage 4 part 1: existing screens reflect an applied plan; stop calling a closure |
| 4 | `6695718` | df-frontend | 3903 | 2026-10-03 14:16:46 | Stage 4 part 2: the Service recovery screen |
| 4 | `d268821` | df-frontend | 4000 | 2026-10-03 14:25:12 | Stage 4: verify the DST-spanning closure I had flagged as untested |

Commits that touch nothing under `stage-N/` (mandates, docs, `room.json`):

- `c558455` (df-coordinator, index 74, 2026-10-03 04:49:29 UTC): Initial commit: seed .gitignore and seat mandates before Stage 1 dispatch
- `ca60ade` (me): adds `room.json`. It is made after the export, so it cannot appear in `room.json`. The commits that add `README.md` and `FACTORY.md` are made by me for the same reason.

## 9. Known limits

- Accessibility got axe-core static analysis plus manual keyboard and contrast checks. Nobody used a real screen reader.
- Every screen was rendered in headless Chromium only. Firefox, Safari and a physical touch device never saw it.
- The spec's 50 concurrent in-flight requests and its latency budget were not load-tested at those numbers. Races were shown with real concurrent requests at 10 to 20 callers, on top of a single global store mutex.
- Recurring-series amendment has no browser screen. Only the API exercises it.
- The Service recovery screen came from my dispatch, not from the spec. It was built after the required behaviour landed.
- The official harness says it runs only part of the judging tests. The seats' own suites go further, but I can't see how much of the judging suite they cover.
- `README.md`, `FACTORY.md` and `room.json` are mine. Every dispatch said the seats wouldn't write them.
