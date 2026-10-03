# Stage 2 frontend — render passes, what each one changed, and the final scores

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
