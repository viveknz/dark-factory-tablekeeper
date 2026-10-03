# Tablekeeper — design system and screen layouts (stage 2)

Written by `df-frontend` before any screen code, per the frontend mandate. The source of truth is
`VISUAL-BRIEF.md` and the six reference screenshots in the input folder
(`C:\Users\vivek\df-inputs\reference\`). Where my taste and the brief differ, the brief wins. Where
the brief and the official stage-2 spec differ, the spec wins (the brief says so itself).

Stage 4+ should start from this file and extend it rather than restating it.

> **Stage 3 (this stage).** No new screens; the visual system, type scale, spacing, colour roles
> and every screen layout below are unchanged and were re-verified against the stage-3 service.
> Three things did change, all in section 10 at the end of this file: the screens now select the
> same **published policy** the server applies, an unavailable cell now says **why**, and the
> legend's third swatch reads "Unavailable" rather than the reference's "Taken".

---

## 1. Identity

| | |
|---|---|
| Product name, as shown | **Tablekeeper** |
| Tagline | **Find a table. Keep it held.** |
| Eyebrow above the headline | RESTAURANT RESERVATIONS |
| Hero supporting line | "Pick a night, choose a table and we will hold it for you. Two minutes, no phone call." |

**Voice.** Calm, welcoming, plain. Short sentences. Second person. We say what happened and what to
do next, never an error code. No exclamation marks, no emoji, no invented reviews, prices or
amenities, no ornament for its own sake.

Examples of the voice, taken from the states this stage has to render:

- Nothing chosen yet → "Select an available time to review your reservation here."
- Signed out, slot chosen → "Sign in to hold this table." / "Your choice, Booth at 19:00, is kept while you sign in."
- Taken by someone else mid-booking → "That table was taken a moment ago. We have refreshed the times — your details are still here, just pick another."
- Lost response → "We did not hear back. Your table may or may not be held. Nothing has been double-booked — press Confirm booking again and we will check."
- No restaurants loaded → "No restaurants yet." + plain-words pointer to `demo/DEMO.md`.

**Feeling.** A warm, quiet hospitality product: cream paper and ink in light, deep ink and sand in
dark. It should read as something a restaurant group would put its name on, and a stranger should
know to pick a night and press **Find tables** within five seconds.

---

## 2. Visual system

### 2.1 Colour roles

Values are the brief's, unchanged. Implemented as CSS custom properties on `:root` and
`[data-theme="dark"]`; no component hardcodes a hex.

| Role | Token | Light | Dark |
|---|---|---|---|
| page | `--page` | `#fbf8f2` | `#14131c` |
| surface | `--surface` | `#ffffff` | `#1d1c28` |
| text | `--text` | `#1f1d2b` | `#f2efe8` |
| muted text | `--muted` | `#5b586b` | `#b3afc0` |
| primary action fill | `--primary` | `#2f2e41` | `#e2bf94` |
| primary action text | `--on-primary` | `#ffffff` | `#1b1a24` |
| accent (selected, highlight) | `--accent` | `#e2bf94` | `#e2bf94` |
| success | `--success` | `#2f6b4f` | `#6fcf9d` |
| warning | `--warning` | `#8a5a12` | `#f0c36d` |
| danger | `--danger` | `#a63d40` | `#f08a8d` |
| focus ring | `--focus` | `#c58b3a` (3px) | `#e2bf94` (3px) |

Derived roles (not in the brief, needed for borders/hover and kept inside the brief's family):

| Role | Token | Light | Dark |
|---|---|---|---|
| hairline border | `--line` | `#e7e1d6` | `#2e2c3c` |
| strong border (inputs, cells) | `--line-strong` | `#c9c2b4` | `#3f3d50` |
| sunken / taken cell fill | `--sunken` | `#f4f1ea` | `#232231` |
| accent text on accent fill | `--on-accent` | `#1b1a24` | `#1b1a24` |
| edge of an accent-filled control | `--accent-edge` | `#2f2e41` | `#e2bf94` |

Contrast is measured for every pairing in both themes; the numbers are in `NOTES.md`. The accent
`#e2bf94` is a light sand: it is only ever used as a *fill* behind `--on-accent` ink, or as a
border/ring — never as text on `--surface` or `--page`, in either theme, because it would fail
contrast there.

Two of the brief's values do not clear WCAG's 3:1 non-text minimum in the **light** theme on their
own, which measuring turned up (see `NOTES.md` pass 4):

- the accent **as a fill** is 1.73:1 against white, so an accent-filled control (the chosen cell,
  the searched day) takes its boundary from `--accent-edge` instead — 13.22:1. The brief's accent
  stays the fill; only the edge changes.
- the focus ring `#c58b3a` is 2.78:1 against the page. The ring stays the brief's gold; a 1px line
  of `--text` is drawn just outside it (`box-shadow: 0 0 0 6px var(--text)`), so the indicator's
  outer boundary is 15.61:1. The result is a two-tone ring, dark/gold/dark.

### 2.2 Type

System stacks only — no font files, no network.

```
--font: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto,
        "Helvetica Neue", Arial, "Noto Sans", sans-serif;
```

Scale, ratio ~1.25, body 16px, nothing under 14px except small labels at 12px (the brief's rule):

| Step | Size / line-height | Weight | Used for |
|---|---|---|---|
| `--fs-display` | 48px / 1.05 | 700 | Hero headline ("Find a table. Keep it held.") — 34px at 375px |
| `--fs-h1` | 30px / 1.15 | 650 | Screen titles ("Search a night", "Sign in", "Review and confirm") |
| `--fs-h2` | 22px / 1.2 | 650 | Section titles ("Saturday 10 October") |
| `--fs-h3` | 18px / 1.3 | 650 | Card titles, table names |
| `--fs-body` | 16px / 1.5 | 400 | Body copy, inputs, buttons |
| `--fs-sm` | 14px / 1.45 | 400 | Secondary lines ("Up to 4 guests"), cell times |
| `--fs-xs` | 12px / 1.3 | 600, 0.08em tracking, uppercase | Eyebrows only (RESTAURANT RESERVATIONS, YOUR RESERVATION, START WITH THE DETAILS) |

Headings semibold (650) with tight line height, per the brief. 12px is reserved for uppercase
eyebrow labels; no sentence-case body text is ever under 14px.

### 2.3 Spacing, corners, elevation

- Spacing scale (4px base): 4, 8, 12, 16, 20, 24, 32, 40, 56, 80 — tokens `--s1`…`--s10`.
- Corners: `--r-sm` 8px (cells, small controls), `--r-md` 12px (inputs, buttons),
  `--r-lg` 16px (cards, panels), `--r-full` 999px (theme toggle, pills).
- Elevation: flat by default. Cards are `--surface` with a 1px `--line` border and no shadow in
  dark; in light they carry one soft shadow `0 1px 2px rgba(31,29,43,.05)`. **No gradients on
  cards** (brief). The sticky review panel gets `0 6px 24px rgba(31,29,43,.07)` in light only.

### 2.4 Controls

| Control | Resting | Hover | Focus | Active | Disabled |
|---|---|---|---|---|---|
| Primary button | `--primary` fill, `--on-primary` text, 12px radius, 44px min height | brightness shift via `--primary-hover` | 3px `--focus` ring, 2px offset | translateY(1px) | 45% opacity, `not-allowed`, `aria-disabled` |
| Secondary button | transparent, 1px `--line-strong`, `--text` | `--sunken` fill | same ring | same | same |
| Text input / select | `--surface`, 1px `--line-strong`, 12px radius, 44px min height, 16px text | border → `--text` | same ring | — | — |
| Slot cell (available) | `--surface`, 1px `--line-strong`, 8px radius, min 44×44 | border → `--accent`, 1px lift | same ring | — | n/a — taken slots are not controls |
| Theme toggle | 40px circle, 1px `--line-strong` | `--sunken` | same ring | — | — |

Every interactive element has all five states defined. Focus is `:focus-visible` with the composite
ring above and is never removed. Two exceptions, both forced by Chromium's shadow DOM and both
verified by eye:

- `input[type="date"]` is styled on `:focus`, not `:focus-visible`, because keyboard focus lands on
  the field's inner day/month/year segments where `:focus-visible` does not match the host.
- the field's calendar glyph is a separate tab stop inside the shadow DOM; it is ringed through
  `::-webkit-calendar-picker-indicator:focus`.

**Nothing is ever given `disabled` or `aria-disabled`.** Both make an element "not enabled" to
automation, so a test that clicks it waits for it to become enabled and then fails — including the
stage's own Playwright harness clicking an unavailable slot to prove nothing happens. In-flight
buttons use `aria-busy` and guard themselves in script instead.

### 2.5 The three cell states (not colour alone)

The brief and the spec both require unavailable ≠ available by more than colour. Three
simultaneous, redundant signals on every cell:

| State | Fill / border | Shape & texture | Text | Markup |
|---|---|---|---|---|
| Available | `--surface`, solid 1px `--line-strong` | flat | time, e.g. `19:00` | `<button>`, `data-available="true"`, `aria-label="19:00, Booth, up to 4 guests — available"` |
| Taken | `--sunken`, **dashed** 1px border | 45° **diagonal hatch** CSS gradient | time with **line-through** | `<span>` — a taken slot is not a control. `data-available="false"`, plus visually-hidden ", Booth, up to 4 guests — already taken" |
| Your choice | `--accent` fill, 2px `--accent-edge` border | — | **✓ glyph** + time | `<button aria-pressed="true">`, label ends "— your choice" |
| Yours, now held | `--accent` fill, 2px `--accent-edge` border | — | **✓ glyph** + time | `<span class="cell-yours">`, `data-available="false"`, hidden text "— your table, held". Rendered after a booking, when the availability refresh shows the slot as taken *by this person* |

So: available is flat+solid+plain, taken is hatched+dashed+struck-through, chosen is filled+ticked.
Each is distinguishable in greyscale and each is announced in text. A legend above the grid names
the three with the same swatches.

### 2.6 Motion

- Transitions 120–180ms, `ease-out`, on `background-color`, `border-color`, `transform`, `opacity`
  only. No layout animation.
- Loading uses skeleton blocks with a 1.4s shimmer.
- Success: the confirmation panel fades and rises 8px once.
- `@media (prefers-reduced-motion: reduce)` sets every duration to 1ms and disables the shimmer and
  the rise. Nothing conveys meaning through motion alone.

### 2.7 Imagery

Supplied assets only, copied unchanged into `static/assets/` with `NOTICE.md` beside them, placed
exactly where the brief's table puts them:

| Slot | File |
|---|---|
| Home hero | `assets/illustrations/undraw_eating-together_mr7m.svg` |
| Date-and-time step on home (beside "Saturday 10 October") | `assets/illustrations/undraw_booking_8vl5.svg` |
| No restaurants / no slots / no data | `assets/illustrations/undraw_no-data_ig65.svg` |
| Booking confirmed | `assets/illustrations/undraw_confirmation_31jc.svg` |
| Login and signup | `assets/illustrations/undraw_login_weas.svg` |
| Top bar logo, favicon, app icon | `assets/brand/tablekeeper-icon.svg`, unchanged, ≥28px in the bar |

Every illustration is `<img>` with `width`/`height` and `object-fit: contain`, so it is shown fully
and never stretched. Decorative ones carry `alt=""`; the no-data one carries real alt text because
it is the only thing on screen. Where the brief lists no image, the slot gets type and whitespace.
No hand-drawn scenes, no photographs, no emoji icons.

**Icons — judgement call.** The brief names Tabler (MIT). Tabler is not in the input folder and
there is no outbound network, and I may not add a dependency the service image lacks. So the six
glyphs this stage needs (sun, moon, calendar, search, check, alert) are drawn by me as inline SVG
from plain primitives — circle, line, polyline — on a shared 24×24 grid with 1.75px round-capped
strokes and `currentColor`, which is Tabler's geometry convention. They are **original**, not copies
of Tabler's paths, so no Tabler licence file is shipped, because shipping one would misstate their
origin. This is recorded in `static/assets/ICONS.md` and in the handoff.

---

## 3. Layout, per screen

Mobile-first: one column at 375px, widening at 768px (`--bp-md`) and 1120px (`--bp-lg`). Page
container max-width 1200px with 24px gutters (16px at 375px). No horizontal page scroll at any
width; the only scrollable sub-region is the results grid, which scrolls on its own X axis with the
table-name column pinned.

### 3.1 Top bar — every screen, sticky

Brand mark + "Tablekeeper" left · **Find a table** / **Reservation lookup** centre · signed-in name
with **Sign out**, or **Sign in** + **Create account**, then the sun/moon toggle, right.

- **No "Service recovery" item this stage.** The reference screenshots show one; `REFERENCE.md` says
  it exists only from stage 4. The spec wins. Deliberate difference from the reference.
- 375px: brand + toggle on the first row, the two nav links on a second row, auth controls on a
  third; nothing wraps mid-word, nothing is clipped.
- `position: sticky; top: 0`. The bar is 64px; every focusable field uses `scroll-margin-top: 80px`
  so the bar never covers a focused field (brief).
- The signed-in name is `data-testid="current-user"` and is present on **every** screen when signed
  in, including `/login`, `/signup` and `/lookup`.

### 3.2 Home `/` — the whole booking flow, one page

Search, results, review *and* confirmation all live on `/`. Choosing a cell updates the review panel
in place; confirming replaces the panel contents with the success state. No page change, no modal,
no wizard. Signing in is the only thing that leaves `/`, and it returns to the same chosen slot.

**1280px / 768px** (768 keeps the structure, narrower):

```
┌─ sticky top bar ──────────────────────────────────────────────┐
├─ hero ────────────────────────────────┬─ eating-together.svg ─┤
│  eyebrow / display headline / 1 line  │                       │
├─ search card "START WITH THE DETAILS / Search a night" ───────┤
│  [Restaurant ▾] [Date] [Party size]        [Find tables]      │
│  ── Open this week · Times in Australia/Melbourne ────────────│
│  Mon 17:30–22:00  Tue …  (searched day highlighted)           │
├─ results card ───────────────────────┬─ review panel ─────────┤
│ booking.svg │ Saturday 10 October    │ YOUR RESERVATION       │
│             │ Harbour Table · 2 …    │ Review and confirm     │
│ legend: Available / Your choice / Taken │ restaurant/date/    │
│ ┌ Table ┆ 17:30 18:00 18:30 … ──────┐│ time/table/party      │
│ │ Window 1  ┆ [17:30][18:00]…       ││ [Confirm booking]     │
│ │ Up to 2   ┆                       ││ hold note             │
│ │ …                                  ││ (sticky, top 80px)    │
│ └────────── scrolls X, names pinned ┘│                        │
└───────────────────────────────────────┴───────────────────────┘
```

Grid is `grid-template-columns: minmax(0,1fr) 340px` at ≥1120px; review panel `position: sticky;
top: 80px`, which is the brief's "sticky under the bar on desktop". Results region is bounded
(`max-height: 62vh`) and scrollable on wide screens, per the brief, with the time header row sticky
inside it.

**375px**: single column, in DOM order hero → search card → results → review panel. The review panel
sits **directly below the results** (brief) and is not sticky. The opening-hours strip becomes a
2-column list. The grid scrolls horizontally inside its own card with the table-name column pinned —
the *page* never scrolls sideways.

Primary action: **Find tables** before a search, **Confirm booking** once a slot is chosen.

**Row labels** carry the human label and capacity on two lines — "Window 1" / "Up to 2 guests";
combinations read "Window 1 + Window 2" / "Combined, up to 4 guests". Technical ids appear nowhere
on screen; they appear only in `data-testid`.

**Beside the grid**: the restaurant's IANA timezone ("Times in Australia/Melbourne") and all seven
days' opening hours, with the searched weekday highlighted — units, zone and range next to the data,
and the *full* week, not a clipped part.

### 3.3 Review panel — five states, same box

| State | Contents |
|---|---|
| Idle (nothing chosen) | "Select an available time to review your reservation here." |
| Chosen, signed in | restaurant / date / time / table / party size rows + **Confirm booking** + hold-and-cancel note |
| Chosen, signed out | the same rows, then an `auth-error` box — "Sign in to hold this table." / "Your choice, Booth at 19:00, is kept while you sign in." + **Sign in** / **Create account**. No Confirm. (This is the signed-out reference screenshot exactly.) |
| Failed / uncertain | `booking-error` or `booking-uncertain` box above the still-intact form |
| Confirmed | confirmation illustration, reference in large type, details, what to do next — and the booking form stays on screen below it, per the spec |

### 3.4 `/login` and `/signup`

Two columns at ≥900px: `undraw_login_weas.svg` left, a bordered form card right, with a visible gap
(64px) between them — the sign-in reference exactly. Fits 1280×720 without scrolling. Single column
at 375px, illustration above the card at a reduced height, scrolling naturally.

- `/login`: WELCOME BACK / **Sign in** / "Sign in to hold your table. Your chosen time is kept." /
  Email / Password / **Sign in** / "New here? Create an account".
- `/signup`: same frame — FIRST TIME HERE / **Create your account** / Name / Email / Password
  (with "At least 8 characters" help text) / **Create account** / "Already have an account? Sign in".
  Three fields only: no role picker, no "become a manager", no sign-in-as-someone-else button, no
  credentials in the browser code (brief).

Both carry the chosen slot through in the URL (`?next=/&slot=…`) so sign-in returns to the same
cell. Nothing but the slot identity is carried.

### 3.5 `/lookup`

Single centred column, max 720px. Reference field + **Find reservation**. Result is a card with the
status as a prominent pill (`confirmed` / `cancelled` — the pill's text is exactly the status word),
restaurant, date, time, **every table label**, party size, and **Cancel reservation** where allowed.
Not-found and refused-cancel both render `reservation-error` in plain words. Cancelled state hides
the cancel button and shows the status pill as cancelled with an icon as well as colour.

### 3.6 First run — no data

When `GET /restaurants` returns an empty list, the home page replaces hero+search+results with one
centred card: `undraw_no-data_ig65.svg`, "No restaurants yet.", a short paragraph explaining that
Tablekeeper shows the restaurants an operator has set up and that this service starts empty, and a
plain-words pointer naming the file: *"The demo instructions are in the `demo/DEMO.md` file in this
project — it has the exact commands to load two restaurants and both demo logins."* The top bar and
theme toggle stay. No fake restaurants are ever invented.

### 3.7 Other states

Loading = skeleton rows in the grid's shape. Closed day / no slots = `no-slots` with the no-data
illustration and "Harbour Table is closed on Sunday 11 October." or "No times left that day."
Fully booked = the grid renders with every cell taken plus a line above it saying so. Refused =
`booking-error` in `--danger`. Uncertain = `booking-uncertain` in `--warning`, with an alert glyph —
never colour alone. Every one of these is a distinct layout, not just a recoloured sentence.

---

## 4. Reference match

| Screen / state | Reference followed | Deliberate differences |
|---|---|---|
| Home signed in, slot chosen | `home-signed-in-light.png`, `home-signed-in-dark.png` | **No "Service recovery" nav item** — stage 4 only, per `REFERENCE.md` and the brief. Slot count, names and taken cells come from the API, not the sample's nine slots. |
| Home signed out, slot chosen | `home-signed-out-light.png`, `home-signed-out-dark.png` | Same nav difference. `auth-error` box, Sign in + Create account, no Confirm — matched as drawn. |
| `/login` | `sign-in-light.png`, `sign-in-dark.png` | None. Matched: illustration left, bordered card right, gap, eyebrow + title + sub, two fields, full-width primary, "New here? Create an account". |
| `/signup` | `sign-in-*.png` (brief: "`/signup` uses the same layout with name, email, password") | Three fields instead of two; eyebrow and copy changed. Frame identical. |
| `/lookup` | Not drawn. Built in the same visual language: same bar, card, type scale, control styles. | — |
| Success, empty, closed-day, fully-booked, loading, refused, uncertain | Not drawn (`REFERENCE.md` lists them as required but undrawn). Same visual language. | — |

The reference images are 1280px. At 768px and 375px the same blocks stack in the same order and
nothing is clipped, per `REFERENCE.md`.

---

## 5. Rules this build will not break

- **No hardcoded service content.** Restaurant names, table labels, capacities, opening hours,
  timezone, slot times, references and statuses all come from the API. Nothing in the markup names a
  restaurant or a table. The screens must be correct for data I have never seen — including a
  restaurant with one table, a 40-slot day, or a 20-character table label.
- **Every `data-testid` from the spec, with the spec's exact semantics** — including
  `slot-{t_a}+{t_b}-{HH:MM}` for combinations in `combinable` order, `data-available` matching
  `available_table_ids` / `available_options` for the party size that was *searched*,
  `confirmation-reference` whose text is exactly the reference with no surrounding words, and
  `reservation-status` whose text is exactly `confirmed` or `cancelled`.
- **No new dependency and no build step.** Hand-written HTML, CSS and ES2019 JavaScript, no
  framework, no bundler, no network at run time. It is served by the existing `http.FileServer`.
- **No Go source touched.** The four HTML routes are an interface change requested from the
  implementer through the coordinator.
- 375px first. Every input has a visible `<label>` (never placeholder-only). Focus always visible.
  Touch targets ≥24px, ≥44px for primary controls. Contrast checked in both themes.


---

## 10. Stage 3 additions

Stage 3 added no screens. It widened the service underneath the existing ones, and two of those
widenings made the screens say things that were no longer true.

### 10.1 Published policies — the screens select the same policy the server does

From stage 3 a restaurant can publish dated policies that change opening hours, capacities,
slot length, duration and cancellation cutoff. The catch is deliberate in the spec:

> `GET /restaurants/{id}` still returns its **original fixture configuration**. Availability and
> booking decisions use the **selected policy**, not that detail.

The stage-2 screens read only the detail, so with a policy published they printed opening hours
and capacities that contradicted the grid sitting right beside them (measured: hours strip
"17:30–22:00" above a grid of 12:00/13:00/14:00 slots, and a row reading "Up to 2 guests" next to
a cell that was available for a party of six). Fixed by selecting the policy client-side exactly
as the server does:

- `GET /restaurants/{id}/policies` is public; it is fetched inside the **same freshness-sequenced
  operation** as the restaurant detail and the availability, so a stale policy list can never be
  applied over a newer search.
- `termsForDate(ymd)` picks the greatest `effective_from` not later than that date, ties broken by
  the greatest `policy_version`, falling back to the fixture's own rules (policy 0). `effective_from`
  is `YYYY-MM-DD`, so a string compare is a correct date compare.
- It is treated as **optional**: a service that publishes no policies, or a failed request, simply
  leaves policy 0 in force. The screens still work against a stage-1 or stage-2 service.

What now reads from the selected policy rather than the fixture:

| Shown | Policy selected for |
|---|---|
| the opening-hours strip, **per day** | each of the seven dates in the searched week — a policy may take effect mid-week, and the strip shows the old hours before it and the new hours from it on |
| a row's capacity, and a combined row's summed capacity | the searched date |
| "The table is held for … / you can change or cancel until …" | the **booking's own start date**, which is the policy whose terms that booking would accept |
| the closed-day copy on the `no-slots` screen | that date |

Table ids, labels, timezone and declared combinations cannot be changed by a policy, so those
still come from the restaurant detail.

### 10.2 An unavailable cell says why

Stage 3 states that availability is decided by exactly two independent rules — `capacity` (the
party fits) and `no_overlap` (nothing confirmed clashes) — and exposes both per table through
`GET /availability?...&explain=true`.

Before this, **every** unavailable cell announced "already taken", which is false for a table that
is merely too small for the party. Now:

- the availability request asks for `explain=true` and the server's answer is the authority;
- when it is absent (an older service, or a stubbed response) the same two rules are derived from
  what is already on screen: a row that fits the party can only be unavailable because something
  clashes;
- the words are plain and never the raw rule name: **"already booked"**, **"too small for your
  party"**, or both. The `policy_version` that `explain` also carries is never rendered.

Where it appears:

- **once per row**, visibly, when the table can never fit this party: a third line under the
  capacity, in `--warning`, reading "too small for 6 guests". Said once rather than repeated into
  every cell.
- **per cell**, as the visually-hidden state text and the `title`, so the hidden text is accurate
  rather than uniformly "already taken".

### 10.3 The legend's third swatch

It reads **"Unavailable"**, where the reference screenshots read "Taken". Deliberate: from stage 3
a row can be hatched because the table is too small for the party, which is not the same thing as
taken, and the row itself now says which. A legend that said "Taken" about those rows would be
stating something false. This is the only wording in the build that departs from the reference.

### 10.4 Still true, re-verified

- **No "Service recovery" item in the top bar.** Stage 4 only.
- **No raw API field is ever rendered** — not `policy_version`, `revision`, `accepted_terms`,
  `effective_from`, `capacities`, `series_id`, nor any raw rule name. Verified against the
  rendered DOM text of every screen and state, not just the source.
- Every `data-testid`, route and behaviour from stage 2 is unchanged.
