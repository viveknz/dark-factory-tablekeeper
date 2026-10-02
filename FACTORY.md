# FACTORY.md

How to stand up the factory that built this repository, what it cost, and how it handles bad work.

## 1. The band

Three seats in one BAND Desktop room. All run **Claude Code** (CLI) as separate sessions. Mandates are in `mandates/`, named after each seat.

| Seat | Owns | Does not |
|---|---|---|
| coordinator | Reads the stage task, hands it to the others, accepts or rejects the result, reports to the human | Write code |
| implementer | Builds the stage in the result repository, runs the supplied checks, commits | Accept its own work |
| reviewer | Re-runs the checks independently at the committed revision, reads the code, says accept or changes needed | Fix the code |

The mandates are generic. They contain no track vocabulary, so the same three files can point at a different problem.

The files were renamed from `coordinator.md`, `implementer.md` and `reviewer.md` to match the room's seat names (`tk-` prefix) after the run. Git records a pure rename with no change to the contents. The text still uses the template handles `@implementer` and `@reviewer`.

**Model and harness:** Claude Code, model `claude-sonnet-5`. The model was not pinned in BAND (the Model field is left on "Provider default"), so I checked what actually ran: all 1,306 assistant messages in the seats' Claude Code session logs report `claude-sonnet-5`.

**Runtime settings (BAND, per seat):** context `local_config` (the seat inherits the host's `~/.claude`: hooks, skills, CLAUDE.md and MCP servers), permission mode `auto`, no denied tools, reasoning effort left at the runtime default. A team reproducing this should start from a clean `~/.claude`, or the seats will pick up whatever is configured on their host.

## 2. Setup

1. Install BAND Desktop and register three Claude Code agents, one per seat.
2. Create a room. Add the three agents.
3. Create an empty result repository. Give the seats its **absolute path**: a seat works in its own sandbox and cannot resolve relative paths.
4. Set a git author for the repository.
5. Dispatch a stage: one message to the coordinator holding the full stage spec, the repository path, the previous accepted commit, the `stage-N/` target, and the rule below. Paste the spec in full. Do not paraphrase it.
6. Send nothing else until the coordinator's final report.

The spec is fetched byte-exact from the official repository on the machine that sends the message, never retyped.

## 3. Design choices and why

**Self-contained handoffs.** A seat only sees messages addressed to it. Every handoff therefore carries the complete requirements, repository path, commit and commands. The mandates forbid "read the room" or "see message 12" as a handoff. This was the biggest single choice: it is what let three separate sessions work without shared memory.

**Review is independent and separate from building.** The reviewer re-runs the checks itself and does not fix code. The coordinator accepts only the commit the reviewer actually checked.

**One human input per stage.** After dispatch, no seat may ask me for anything. Blockers go in the final report instead of a question. The room log holds exactly four human messages, one dispatch per stage.

**Dependency-free service.** The service uses only Node built-ins with hand-written HTML/CSS/JS. It builds with no network and runs from a clean container.

**Stages are folders, not branches.** Each `stage-N/` is the previous stage carried forward and widened. Earlier folders are never touched once accepted. After stage 4, a `git diff` of stages 1-3 against the stage-3 commit came back empty.

**The mandates are the stock minimal template.** I did not tune them. The factory's quality comes from the handoff and review discipline above, not from clever prompting. That has a cost: nothing in the mandates tells a seat how to spot its own weak work.

## 4. What went wrong and how it was handled

| Problem | Handling |
|---|---|
| The `--mode isolated` harness fails on Windows (backslash path passed to a Linux container). Not a code defect. | Verified with `--mode host`. Told to the seats in every dispatch. |
| The reviewer hit the Claude session limit mid re-review of the stage 2 fix (12:43 UTC, reset 2:30am Sydney), a stall of about four hours. | Restarted the seat's process (not the room); no human message sent. The reviewer's first reply after the stall said the repo was not at the reported revision. The coordinator and implementer confirmed `a13aa57` in the room, and the reviewer accepted it at 16:53 UTC. |
| All three seats run on one Claude subscription, so they share one usage quota with each other and with any other session. | Not solved. Stages are run one at a time, and a stalled seat is restarted. |
| Stage 2 build `98d259b` had a defect: the booking-uncertain state rendered with the red error style instead of the amber uncertain style. The reviewer found it live and returned "accept pending one fix". | The coordinator relayed it to the implementer, who fixed it in `a13aa57` (one file, +5/-3). The reviewer re-verified and accepted. This is the one rejection in the run that changed the code. |

The implementer also runs its own deeper checks beyond the shipped harness. In stage 4 its check script failed three runs in a row (22:53-22:55 UTC) before passing. Each failure was an error in the script (a wrong expected value, a restaurant left with no free table, a wrong revision snapshot). Only the script was edited between those runs.

## 5. How the factory catches bad work

1. The implementer runs the shipped harness before handing off.
2. The reviewer runs it again at the reported commit, then reads the code and probes behavior the shipped checks do not cover. For stage 4 that meant replan optimality, preview isolation, stale plans, concurrent applies and series-amend atomicity.
3. The coordinator will not accept a commit the reviewer did not check.
4. I re-run the official harness myself after every stage. I never rely on a seat's self-report.

## 6. Time and cost

Commit times (local, AEST):

| Stage | Commit | Time |
|---|---|---|
| 1 | `e17fe5b` | 27 Sep 18:05 |
| 2 | `98d259b`, fix `a13aa57` | 1 Oct 22:01, 22:39 |
| 3 | `411075a` | 2 Oct 03:13 |
| 4 | `830e4d2` | 2 Oct 08:56 |

Dispatch message to the coordinator's final report, from the room log:

| Stage | Time |
|---|---|
| 1 | 40m |
| 2 | 5h 18m (about four hours of that was the usage-limit stall) |
| 3 | 26m |
| 4 | 31m |

Size of the result (non-blank lines under `src/`): 1,020 / 2,056 / 2,592 / 2,957 for stages 1-4.

**Model spend.** BAND shows **$58.52** on the room header for this run. That is BAND's own figure and I did not check how it is calculated. The seats authenticate against a Claude subscription rather than an API key, so I paid no per-token bill.

## 7. Reusing this on another problem

Point the three mandates at a different spec and dispatch as in section 2. What you would change: the repository path, the spec, and the commands the reviewer runs. The mandates themselves do not need editing beyond the seat names and handles.

## 8. Commit trace

Every commit in `stage-1/` to `stage-4/` traces to a `git commit` tool call in `room.json`. The log holds exactly five such calls, all by `tk-implementer`; no other seat and no human ran `git commit`. Each hash first appears in the output of the implementer's own call. Times are the room log's `insertedAt` (UTC).

| Stage | Hash | Room-log tool call (message index, UTC) | Hash first seen in output |
|---|---|---|---|
| 1 | `e17fe5b` | 149, 27 Sep 08:05:13 | 150, 08:05:16 |
| 2 | `98d259b` | 427, 1 Oct 12:01:20 | 428, 12:01:22 |
| 2 (fix) | `a13aa57` | 733, 1 Oct 12:38:59 | 734, 12:39:01 |
| 3 | `411075a` | 1049, 1 Oct 17:13:02 | 1050, 17:13:04 |
| 4 | `830e4d2` | 1375, 1 Oct 22:56:17 | 1376, 22:56:21 |

Message indexes are positions in the `messages` array (0-based). The later commits (`460b1a6`, `99b3c13`, `f7e0e52`) add the mandates, a rename, and these docs; they touch nothing under `stage-N/`.

To check it yourself, search `room.json` for `git commit` in `tool_call` messages.