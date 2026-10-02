# Dark Factory: Tablekeeper

Entry for the WeAreDevelopers x BAND **Dark Factory** hackathon (online, 26 Sep - 5 Oct 2026).

- **Team:** Vivek (solo), Melbourne, Australia
- **Track:** Tablekeeper (restaurant reservation service)
- **Result:** all four stages built and accepted by the factory's own reviewer. The official harness passes stages 1-4 on the shipped checks (partial suite, host mode).

## What this is

A software factory: three Claude Code seats (coordinator, implementer, reviewer) working in one BAND Desktop room. I dispatched each stage's spec once. The seats built, tested, reviewed and accepted the code without further input from me. The service they built is in `stage-1/` to `stage-4/`.

## How to read this repository

| Path | What it is |
|---|---|
| `FACTORY.md` | The factory: seats, setup, design choices, failures, time and cost |
| `mandates/` | One mandate per seat, each naming its harness and model |
| `room.json` | The full room log, downloaded from BAND unedited |
| `stage-N/` | The service at stage N. Each folder builds on its own and carries the previous stage forward |

Git history is the seats' own commits. Nothing under `stage-N/` was written or edited by hand.

## Run a stage

Each stage folder is a self-contained Node service with no runtime dependencies and no network access needed, at build or run time.

```sh
docker build -t tablekeeper-stage4 stage-4
docker run --rm -p 8080:8080 -e PORT=8080 tablekeeper-stage4
curl http://localhost:8080/health
```

Open `http://localhost:8080/` for the booking UI. `stage-N/RUN.md` has the details for each stage.

## Stages

| Stage | Scope | Shipped checks |
|---|---|---|
| 1 | Reservations API: auth, idempotency, DST, export/import, atomic moves | 120 |
| 2 | Browser booking UI and combined tables | 25 |
| 3 | Booking policies, history, recurring reservations | 7 |
| 4 | Manager table-closure replanning and bulk amendment | 6 |

Stage N is graded against every suite up to N. The official harness passes all four.
