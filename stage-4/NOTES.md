# Tablekeeper frontend — render passes, what each one changed, and the scores

Stage 3's record is first; stage 2's follows it unchanged, because it is still the record of how
these screens were built.

---

# Stage 3 — re-verification, and the two things that stopped being true

Stage 3 required no new screens. The job was to confirm the stage-2 screens still work against the
widened service, and that is mostly what happened — but measuring turned up two places where the
screens had quietly started saying something false, so those were fixed.

## What was carried forward, and proof it was untouched

`stage-3/DESIGN.md`, `NOTES.md`, `static/index.html`, `static/app.css`, `static/app.js` and all of
`static/assets/` arrived byte-identical to my accepted stage-2 commit (`diff` clean on every file,
and `git diff 31273ce HEAD -- stage-2/static stage-2/DESIGN.md stage-2/NOTES.md` empty).

## Re-verification against the stage-3 service, before any change

The stage-3 image was built and run with `static/` bind-mounted, and the whole stage-2 check suite
pointed at it:

| Check | Result against the stage-3 service |
|---|---|
| all four routes + JSON 404 for anything else | `/`, `/login`, `/signup`, `/lookup` → 200 `text/html; charset=utf-8`; `/nope` → 404 `application/json` |
| `behaviour.py` (55 checks) | 55 passed, 0 failed |
| `a11y.py` axe-core, 108 scans | 0 violations at any impact |
| `focus.py` keyboard walk | 0 focus problems |
| `motion.py` | reduce → 0 animating; no-preference → 54 |
| no horizontal scroll, touch targets, font sizes | clean at 375/768/1280 in both themes |
| "Service recovery" in the top bar | **absent**, as required before stage 4 |
| raw API fields in rendered text | **none** — scanned the rendered DOM of every screen and state for `policy_version`, `accepted_terms`, `revision`, `no_overlap`, `capacities`, `series_id`, and for stray `undefined`/`null`/`NaN`/`[object Object]` |

**Pixel evidence that nothing moved.** The full 108-shot render against stage 3 was compared with
the accepted stage-2 render: 84 identical, 24 differing. Rather than hand-wave the 24, stage 3 was
rendered a second time and compared against itself: **exactly the same 24 files differ run-to-run**,
and the set difference is empty. So every difference is inherent nondeterminism — the random
booking reference in the confirmation and lookup shots, and the loading skeleton's shimmer phase —
and there were **zero visual regressions**.

## The defect that confirmation found

`GET /restaurants/{id}` deliberately keeps returning the *original fixture configuration*, while
availability and booking decisions use the *published policy* for the date. The stage-2 screens
read only the detail. Publishing a policy that moved Harbour Table to 12:00–15:00 and raised every
capacity by 4 produced this, measured, not guessed:

- the grid correctly showed slots at **12:00, 13:00, 14:00**, while the hours strip beside it still
  read **"17:30–22:00"**;
- Window 1 was correctly **available for a party of 6**, while its row still read **"Up to 2 guests"**.

The screen contradicted itself in two places at once. The brief requires the week's opening hours
beside the grid and rows labelled with capacity, and my own mandate says a screen must be correct
for data I have never seen — so this is a correctness defect in the existing screens, not an
enhancement, and fixing it is part of "do the screens still work". Fixed per DESIGN.md §10.1;
the hold note now also quotes the terms of the policy the *booking's own start date* would accept.

Proof (`policies.py`, 9 checks, all passing). The decisive one: with a policy effective on the
Sunday, the week strip renders `Mon 17:30–22:00 … Sat 17:30–23:00  Sun 12:00–15:00` — per-day
policy selection, not one policy smeared across the week.

## The optional enhancement: taken, or too small?

Stage 3 decides availability by exactly two rules and exposes both through `explain=true`. Checking
that surfaced a second false statement: **every** unavailable cell announced "already taken",
including tables that were merely too small for the party. That is simply wrong, so it was worth
doing. Built per DESIGN.md §10.2, and the legend's "Taken" became "Unavailable" (§10.3) because the
old word was false for those rows.

`reasons.py`, 6 checks, all passing — including cross-checking **21 unavailable cells' reasons
against the server's own two rules**, that a too-small table never claims to be "already booked",
that an available cell carries no reason, that every cell still shows its time as text, and that no
raw rule name or policy field is rendered.

## The optional enhancement I declined, and why

A reservation-history view on `/lookup`. `GET /reservations/{reference}/history` exists and a
cancelled reservation keeps its history, so it would work. I decided against it:

- nothing in the spec, the brief or the dispatch asks for it — the spec says outright that no new
  screens are required for explanations or history;
- **every history entry carries `revision` and the complete `accepted_terms`** — precisely the
  fields the dispatch singles out as never allowed into rendered text. It is the highest-leakage
  data surface in the stage, added for a read-only convenience nobody requested;
- this stage's remit is confirmation. The two fixes above correct things that were *false on
  screen*; a history panel corrects nothing. I would rather hand over a `/lookup` that is verified
  pixel-identical to its accepted stage-2 self than a new panel whose states only I have ever seen.

If a later stage wants it, DESIGN.md §3.5 is the place to extend, and the only hard rule is that
`revision` and `accepted_terms` are read for logic and never printed.

## After the changes — everything re-run

| Check | Result |
|---|---|
| `behaviour.py` — every stage-2 hook and the competing-client rules | **55 passed, 0 failed** |
| `policies.py` — the screens describe the policy the server applies | **9 passed, 0 failed** |
| `reasons.py` — reasons match the server's rules, nothing raw rendered | **6 passed, 0 failed** |
| `a11y.py` — axe-core, 108 scans | **0 violations at any impact** |
| `focus.py` — keyboard walk, 5 screens × 3 widths × 2 themes | **0 focus problems** |
| horizontal scroll / touch targets / font sizes | **clean** at 375/768/1280, both themes |
| visual regression on the demo data (no policies published) | **zero** — the 23 files that differ are exactly the known nondeterministic set |

## Stage 3 scores

The screens are the stage-2 screens, so the stage-2 scores stand. Two axes moved:

| | Stage 2 | Stage 3 | Why |
|---|---|---|---|
| Home — states | 5 | **5** | holds: the new reasons are a distinct, plainly-worded state rather than a recoloured one |
| Home — reference match | 5 | **4** | one deliberate departure: the legend reads "Unavailable", not the reference's "Taken". Recorded in DESIGN.md §10.3. I am keeping the departure — the reference word is false for a capacity-excluded row, and I will not print something false to match a sample |
| Home — hierarchy & layout | 5 | **5** | the row reason is a third line in an existing block; no layout moved, verified pixel-identical |

Everything else is unchanged at 5, with craft & motion still 4 for the reason given in the stage-2
section below.

## Stage 3 — what I did not check

Everything in the stage-2 "What I did not check" section still stands (no real assistive tech, no
non-Chromium browser, no physical touch device). In addition, for stage 3:

- **Recurring series and collective moves have no UI**, by design — no screen is required for them
  and I built none, so I have not exercised `POST /series`, `GET /series/{id}` or
  `POST /reservation-moves` through the browser at all.
- **Policy selection is verified against published policies I created myself**, covering: no policy
  at all, a policy effective mid-week, a date before any policy, and a policy changing hours,
  capacities, duration and cutoff together. I did **not** test several policies sharing one
  `effective_from` (the tie-break on greatest `policy_version` is implemented and matches the spec's
  wording, but it is reasoned, not observed), nor a policy effective in the past.
- **`explain=true` is only read for the words in an unavailable cell.** `data-available` still comes
  from `available_table_ids` alone, so a wrong explanation could never make a cell wrongly
  clickable. I did not test a service that returns `explain` with a table missing from it; that
  path falls back to the local derivation.

---

# Stage 2 — how these screens were built



Written by `df-frontend`. Everything below is something I ran or a screenshot I looked at. Where I
did not check something, it says so.

Screenshots and check output live **outside** the repository, in `C:\Users\vivek\df-checks\`:

| Path | What |
|---|---|
| `render.py` | renders every screen/state at 375/768/1280 in both themes; checks horizontal scroll, font sizes, touch targets |
| `behaviour.py` | the competing-clients and idempotency rules, driven through a real browser |
| `a11y.py` | axe-core over every screen/state × 3 widths × 2 themes |
| `focus.py` | walks the keyboard path of every screen, probing every tab stop |
| `motion.py` | `prefers-reduced-motion` both ways |
| `stage-2-render-1/` … `stage-2-final/` | 108 PNGs per pass, plus `checks.txt` |
| `a11y-report.txt`, `contrast.txt`, `behaviour-final.txt` | raw output |

The service under test ran as the stage's own Docker image with `static/` bind-mounted, so every
screenshot is of the real binary serving the real files:

```sh
docker build -t tk2 .
docker run -d --name tk2 -p 8099:8080 -e DEMO_SEED=1 \
  -v "C:/Users/vivek/df-tablekeeper/stage-2/static:/static:ro" tk2
```

---

## Pass 1 — first render (`stage-2-render-1`)

Rendered 18 scenarios × 3 widths × 2 themes. Two defects fixed before I looked at anything:

1. **The results card stretched the page at 375 and 768.** `document.scrollWidth` was 794 at a
   375px viewport. A grid item's default `min-width` is its content, so the wide availability table
   pushed its card — and the page — past the viewport. Fixed with `minmax(0, 1fr)` on `.work` and
   `.results` and `min-width: 0` on the scroll region.
2. **Inline links were 19px tall**, under the 24px minimum. `.link` became `inline-block` with
   vertical padding.

## Pass 2 — looking at the screenshots (`stage-2-render-2`)

Overflow and target checks clean, so I opened the images next to the references. Three real bugs,
all invisible to the automated checks:

1. **The `hidden` attribute did nothing on anything I had styled.** `hidden` is a UA
   `display: none`, so any author `display` rule beats it. `.work`, `.hours`, `.legend` and the
   first-run card are all `display: grid`/`flex`, so **the "No restaurants yet" card rendered on a
   page that had two restaurants**, and the theme toggle showed the sun *and* the moon at once.
   Fixed with one rule: `[hidden] { display: none !important; }`.
2. **The grid clipped a column at 1280.** Seven slots did not fit in the results card; the seventh
   was sliced in half. Cell padding and `min-width` came down (56→48px) and cell/`td` padding
   tightened, so all seven fit with room to spare.
3. **A stray "·" after the highlighted weekday.** I had used `::after { content: " ·" }` as a
   non-colour signal; it read as a typo. Replaced with bold weight plus visually-hidden
   "— the night you searched".

Also fixed the screenshots themselves: a full-page capture paints a sticky bar wherever it is
stuck, so the top bar appeared floating mid-page. `render.py` now scrolls to the top first.

## Pass 3 — phone and tablet (`stage-2-render-3`)

1. **The pinned table-name column ate 40% of a 375px screen**, leaving one and a bit time columns
   visible. Narrowed to 112px under 768px: three columns now in view.
2. Hero top padding tightened at desktop to sit closer to the reference.
3. **A behavioural defect found by `behaviour.py`, not by looking**: typing in the party-size field
   re-rendered the whole review panel, destroying the input under the caret. Focus was thrown away
   mid-edit. The two notes that depend on the value are now updated in place; the panel is not
   re-rendered on keystroke.
4. **The one that mattered most.** `behaviour.py` hung for 30s and failed on "clicking an
   unavailable cell does nothing". Playwright treats `aria-disabled="true"` as *not enabled*, so
   `click()` waits for it to become enabled and then times out — and the stage's own harness may
   well click an unavailable slot for exactly this assertion. I had chosen `aria-disabled` over
   `disabled` specifically to avoid this and it was not enough. **A taken slot is no longer a
   control at all**: it renders as a `<span>` carrying the testid, `data-available="false"` and
   visually-hidden state text. Clicking it does nothing because there is nothing to click, and
   nothing has to block to make that true. `aria-disabled` was removed from the whole build; the
   in-flight submit buttons use `aria-busy` and guard themselves in script.

## Pass 4 — measuring instead of assuming (`stage-2-render-5`, `-6`)

1. **A horizontal-scroll regression my own checker was hiding.** `document.scrollWidth` was 409 at
   375px. The offender list was empty because I skip anything inside the scrollable grid — so I
   measured properly: `body.scrollWidth` was a correct 375, and the 34px came from the
   visually-hidden `position: absolute` state text inside the cells escaping to the initial
   containing block, because nothing inside the scroll region was positioned. Fixed with
   `position: relative` on `.grid-scroll`. **I also rewrote the check to stop trusting
   `scrollWidth`**: it now tries to actually scroll the window sideways and fails if it moves.
2. **The grid went stale after a booking** — the slot you had just taken still read as free beside
   your own confirmation. A single refresh now runs after a success (not a poll), keeping the
   selection, the inputs and the pending idempotency key intact so an unchanged resubmit still
   replays. The slot you hold renders as *yours* (accent + tick + "your table, held"), not as
   somebody else's "Taken".
3. **The lookup screen gave Cancel equal billing with Change.** They now sit side by side, with the
   destructive one quieter and second.
4. Tablet (600–899px) top bar went from three stacked rows to two.
5. **Contrast, measured.** Three non-text pairings failed 3:1 in the light theme — see the table
   below. Fixed with `--accent-edge` and a composite focus ring, keeping the brief's colours.
6. **A real focus bug my first keyboard walk missed.** The walk reported "0 problems / 7 stops",
   which I did not believe, so I fixed the walker (it was breaking early instead of walking the
   whole order). The honest walk — 59 Tab presses per screen, ~42 distinct stops on home — found
   that **the date field showed no focus ring at all**: Chromium puts keyboard focus on the field's
   inner segments, where `:focus-visible` does not match the host. Fixed by styling `:focus`.
   The field's calendar glyph is a further tab stop inside the shadow DOM; it is now ringed through
   `::-webkit-calendar-picker-indicator:focus`. I confirmed that stop by eye rather than trusting
   the probe, because JS cannot read a pseudo-element's computed style.

---

## Final check results

Every one of these was run against the final committed revision.

| Check | Result |
|---|---|
| `behaviour.py` — competing clients, idempotency, every `data-testid` contract | **55 passed, 0 failed** |
| `a11y.py` — axe-core, 18 states × 3 widths × 2 themes | **108 scans, 0 violations at any impact** |
| `focus.py` — keyboard walk, 5 screens × 3 widths × 2 themes | **0 focus problems** |
| horizontal scroll at 375/768/1280, both themes | **none** — the window cannot be scrolled sideways on any screen or state |
| touch targets | **none under 24px**; every primary action ≥44px |
| body text | nothing under 14px except the 12px uppercase eyebrow labels the brief allows; **nothing under 12px** |
| `motion.py` — `prefers-reduced-motion` | **reduce: 0 elements animate; no-preference: 54 do** |

### Contrast, measured (ratios, not impressions)

Light theme, after the fixes. Every text pairing needs 4.5:1, every non-text boundary 3:1.

| Pairing | Light | Dark |
|---|---|---|
| body text on page | 15.61:1 | 16.05:1 |
| body text on surface | 16.55:1 | 14.64:1 |
| muted text on page | 6.49:1 | 8.61:1 |
| muted text on surface | 6.88:1 | 7.85:1 |
| muted text on sunken (taken cell) | 6.10:1 | 7.30:1 |
| primary button label | 13.22:1 | 9.93:1 |
| label on accent fill (chosen cell) | 9.93:1 | 9.93:1 |
| success text on its tint | 5.56:1 | 7.91:1 |
| warning text on its tint (uncertain) | 5.32:1 | 9.32:1 |
| danger text on its tint (refused) | 5.45:1 | 6.69:1 |
| danger / success text on surface | 6.24 / 6.29:1 | 6.99 / 8.88:1 |
| **focus indicator outer edge vs page** | **15.61:1** (was 2.78:1) | 16.05:1 |
| **accent-filled control edge vs surface** | **13.22:1** (was 1.73:1) | 9.70:1 |

The two "was" rows are the brief's own values failing 3:1 in the light theme. The brief's colours
are still what you see — the gold ring and the sand fill — with a high-contrast boundary added.
Recorded as a judgement call in the handoff.

---

## Scores

1–5 on each axis, scored by me against the reference screenshots and the brief, after the final
pass. Pass-2 scores are from when I first looked at the images.

| Screen | | Identity | Hierarchy & layout | Imagery | Typography | States | Craft & motion | A11y | Responsive | **Reference match** |
|---|---|---|---|---|---|---|---|---|---|---|
| Home `/` | pass 2 | 4 | 3 | 4 | 4 | 2 | 3 | 3 | 2 | 3 |
| | **final** | **5** | **5** | **5** | **5** | **5** | **4** | **5** | **5** | **5** |
| `/login` | pass 2 | 5 | 5 | 5 | 5 | 4 | 4 | 4 | 5 | 5 |
| | **final** | **5** | **5** | **5** | **5** | **5** | **4** | **5** | **5** | **5** |
| `/signup` | pass 2 | 5 | 5 | 5 | 5 | 4 | 4 | 4 | 5 | 5 |
| | **final** | **5** | **5** | **5** | **5** | **5** | **4** | **5** | **5** | **5** |
| `/lookup` | pass 2 | 4 | 4 | 3 | 4 | 4 | 4 | 4 | 4 | n/a |
| | **final** | **5** | **5** | **4** | **5** | **5** | **4** | **5** | **5** | n/a (not drawn) |
| First run / no data | pass 2 | 3 | 3 | 4 | 4 | 3 | 3 | 4 | 4 | n/a |
| | **final** | **5** | **4** | **5** | **5** | **5** | **4** | **5** | **5** | n/a (not drawn) |

Nothing is below 4. The two 4s I am keeping, and why:

- **Craft & motion, 4 everywhere.** Transitions, skeletons, the success rise and a full
  reduced-motion path are all there and verified, but the motion vocabulary is deliberately plain —
  no staggering, no shared-element transitions between the grid and the panel. Richer motion is
  real work with real risk around the booking states, and the brief asks for restraint
  ("nothing ornamental"). I would rather hand over restraint than something half-animated.
- **Imagery on `/lookup`, 4.** The brief assigns no illustration to the lookup screen, so it is
  type and whitespace by design. Correct per the brief, but it is the plainest screen in the set.

`/lookup`, the first-run screen and the success, loading, closed-day, fully-booked, refused and
uncertain states have **no reference screenshot** (`REFERENCE.md` lists them as required but not
drawn), so they are scored against the brief and the rest of the visual language, not against an
image. They cannot have a reference-match score and do not get a made-up one.

## What I did not check

- **Real assistive technology.** axe-core is a static rule engine. I did not drive NVDA, JAWS or
  VoiceOver, so "announced correctly" is an inference from the markup, not an observation.
- **Browsers other than Chromium.** Every screenshot and every check is headless Chromium 153 via
  Playwright. Not opened in Firefox or Safari. The build uses no Chromium-only feature except the
  `::-webkit-calendar-picker-indicator` focus rule, which is additive — other browsers fall back to
  the `:focus-visible` ring.
- **Touch.** Targets are measured in CSS pixels; I did not test on a physical phone.
- **The stage-1 → stage-2 upgrade path end to end.** The implementer owns and has tested the
  export/import guarantee. I verified that my retry keeps its idempotency key and body across a
  lost response, which is the browser-side half; I did not drive an import *between* a lost
  response and its retry.
