# Icons in this build

The approved visual brief names Tabler (MIT) as the line-icon set. Tabler is not in the stage's
input folder (`df-inputs/` contains the brand mark, five unDraw illustrations, `NOTICE.md`, the
reference screenshots and `VISUAL-BRIEF.md` — no icon set), this build has no outbound network at
run time, and the frontend may not add a dependency the service image does not already have.

So the six glyphs the stage-2 screens need are **drawn by `df-frontend` from plain primitives**
(circle, rect, line, polyline) on a shared 24x24 grid with 1.75px round-capped, round-joined
`currentColor` strokes — Tabler's geometry conventions, but original paths:

| Glyph | Where it is used |
|---|---|
| moon / sun | the theme toggle in the top bar (`index.html`) |
| alert (circle + bar + dot) | the uncertain-outcome notice, the fully-booked notice |
| cross in circle | a refused booking, a cancelled reservation's status pill |
| check in circle | the "your table is held" line, a confirmed reservation's status pill |
| check | the tick inside the chosen availability cell |
| magnifier | the "press Find tables" invitation |

They live inline in `index.html` (moon, sun) and in the `ICON` map at the top of `app.js`.

**No Tabler licence file is shipped, deliberately.** These are not Tabler's assets and shipping
Tabler's MIT notice beside paths Tabler did not author would misstate where they came from. There
is nothing third-party here to license: the glyphs are original work made for this project, with no
restrictions on their use.

The brand mark and the five illustrations **are** third-party or supplied assets and are used
unchanged with their attribution intact — see `NOTICE.md` in this folder.
