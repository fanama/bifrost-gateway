---
name: anti-slop-ui
description: Enforce clean, high-craft UI design standards and eliminate generic AI visual tropes in web components and layouts. Use when designing, building, or reviewing UI (HTML, React, Vue, Tailwind, CSS Modules), or when a change adds gradients, card grids, hero sections, badges, or shadows.
---

# Anti-Slop UI

Generic AI interfaces share a recognizable fingerprint: gradient CTAs, floating
card grids, pill badges above every heading, glow effects, centered hero
templates on default Inter. This skill replaces those defaults with decisions
that come from the product's domain.

Craft is mostly subtraction. Before adding a treatment, ask what it
communicates. If the answer is "it looks modern", remove it.

## When to use

- Designing, building or refactoring components, layouts or pages.
- The user asks for a design review or asks why a UI "looks AI-generated".
- A change introduces a gradient, hero section, card grid, badge, shadow,
  rounded-full element or a new font stack.

## Prohibited tropes

Do not emit these. Each one has a specific replacement — use the replacement,
not a milder version of the trope.

| Banned | Why it reads as slop | Use instead |
|---|---|---|
| **The AI gradient** — purple→indigo or cyan→violet linear gradient on text or primary buttons | The single strongest tell; carries no brand meaning | One flat brand colour for the primary action; emphasis from weight, size and contrast |
| **Generic card grids** — 3 equal floating white cards, `rounded-2xl`, `shadow-xl`, on light grey | Cards as the default container flatten hierarchy; equal weight means no priority | Alignment and spacing to group; hairline dividers or a single bordered container. Reserve raised cards for genuinely discrete, clickable objects |
| **Pill badge overuse** — a tiny `rounded-full bg-indigo-100` eyebrow above every section | Decoration pretending to be information | Drop the eyebrow. If it is real metadata (status, count, kind), render it as text with a functional status colour |
| **Neon glow** — `blur-3xl` radial blobs or coloured halos behind cards and dark containers | Adds noise, harms contrast, hides weak layout | Flat surfaces, one border, one shadow tier — or no shadow at all |
| **Cookie-cutter hero** — centered headline, italic serif word, gradient CTA, floating dashboard screenshot with glow | A template rather than a page about this product | State what the product does in plain language, left-aligned, with a real artifact (code, table, data) instead of a fake screenshot |
| **Bland typography** — unconfigured Inter/Roboto, one line-height everywhere, no optical tracking | Untouched defaults signal no design decision was made | A deliberate type scale, modulated line-height, tighter tracking on display sizes, tabular numerals in data |

## Required rules

1. **Intentional palette.** One dominant brand tone, high-contrast neutrals, and
   status colours used only for status. No second accent "for variety".
2. **Purposeful layout.** Vertical rhythm from a spacing scale, not from
   `mt-4` guesswork. Prefer asymmetry, a reading column, or a data-dense grid
   over a centered stack. Center only short, single-purpose content.
3. **Explicit tokens.** Hairline borders, real border colours, defined text
   colours. Depth comes from a border and a single subtle shadow, never from
   shadow alone.
4. **Optical typography.** Display sizes tighten (`-0.02em`), body copy does
   not. Body line-height ~1.6, display ~1.05–1.15. Measure capped near 68ch.
5. **Micro-interactions over effects.** A 120–160ms colour or border
   transition, a focus ring, a small translate on press. No animations that
   compete with content.
6. **Domain alignment.** The layout must look like the product's job. Ship the
   pattern the domain actually uses.

## Token starter

Adapt the values; keep the structure. Tokens make the rules enforceable and
remove the temptation to reach for a gradient.

```css
:root {
  /* one brand tone, deep enough for white text at 4.5:1 */
  --brand: #1f4d43;
  --brand-hover: #173b34;

  /* neutrals, cool and low-chroma */
  --bg: #fbfbfa;
  --surface: #ffffff;
  --border: #e3e3e0;
  --text: #16181a;
  --text-muted: #5c6166;

  /* status only */
  --ok: #17724f;
  --warn: #8a5a00;
  --danger: #a32a24;

  --radius: 4px;            /* one radius, not a ladder of them */
  --shadow-sm: 0 1px 2px rgb(0 0 0 / 0.05);
  --space: 4px;             /* rhythm: 4 · 8 · 12 · 16 · 24 · 32 · 48 · 64 */
}
```

Type pairing must be a decision: a display face used at two sizes, or a single
well-set family with a real scale. Set `font-variant-numeric: tabular-nums` on
tables, metrics and timestamps.

## Layout patterns by domain

| Domain | Correct shape |
|---|---|
| Dev tools, dashboards, admin | Dense tables, monospace identifiers, left rail, row-level actions, visible keyboard focus |
| Editorial, docs, marketing | Single reading column, generous but consistent leading, serif or high-contrast sans, footnote-scale details |
| Logistics, ops, monitoring | Utilitarian grids, fixed columns for status and time, colour reserved for severity |
| Commerce, catalog | Image-led rows, price and availability aligned, filters as a sidebar not floating cards |
| Forms, settings | Label above field, inline validation, one primary action per screen |

## This repository

UI is server-rendered Go templates in `web/templates/`, styled by one `<style>`
block in `web/templates/layout.html`. There is no Tailwind and no build step —
write plain CSS there, reuse the existing classes (`.card`, `.btn`, `.btn.ghost`,
`.badge`, `.grid2`, `.page-head`, `.muted`, `.table-wrap`) and the existing
tokens (`--bg`, `--panel`, `--panel2`, `--line`, `--text`, `--muted`, `--accent`,
`--ok`, `--danger`). The UI is dark-only: check contrast against `--bg`, not
white. Add a token before adding a hard-coded colour.

The examples below are Tailwind because that is the common case elsewhere;
translate them to the tokens above in this repo.

## Worked replacements

Banned:

```html
<section class="text-center py-24 bg-gradient-to-b from-white to-indigo-50">
  <span class="rounded-full bg-indigo-100 text-indigo-700 px-3 py-1 text-xs">New</span>
  <h1 class="text-5xl font-bold bg-gradient-to-r from-indigo-600 to-purple-500 bg-clip-text text-transparent">
    Supercharge your <span class="italic font-serif">workflow</span>
  </h1>
  <button class="rounded-full bg-gradient-to-r from-violet-600 to-cyan-500 px-8 py-3 shadow-xl">Get started</button>
</section>
```

Approved:

```html
<section class="mx-auto max-w-3xl px-6 pt-20 pb-16">
  <h1 class="text-4xl font-semibold tracking-[-0.02em] leading-[1.1]">
    Ingest documents, query them in seconds
  </h1>
  <p class="mt-4 max-w-[60ch] text-[15px] leading-relaxed text-[--text-muted]">
    Parses PDFs and images into grounded chunks. Every answer cites the source page.
  </p>
  <div class="mt-8 flex items-center gap-4">
    <a class="rounded-[--radius] bg-[--brand] px-5 py-2.5 text-sm font-medium text-white transition-colors hover:bg-[--brand-hover] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[--brand]">
      Upload a document
    </a>
    <a class="text-sm text-[--text-muted] underline-offset-4 hover:underline">Read the API docs</a>
  </div>
</section>
```

The difference is not the palette: the approved version makes a claim, uses one
action colour, and has a text hierarchy that survives in grayscale.

## Review checklist

Run this before finishing any UI change. All boxes must hold.

- [ ] Zero gradients on text, buttons, borders or backgrounds.
- [ ] One brand colour; every other colour is a neutral or a status colour.
- [ ] No `rounded-full` except on avatars, icon buttons, toggles and true pills.
- [ ] No `shadow-xl`/`shadow-2xl`; shadows, if any, are one tier and subtle.
- [ ] No pill eyebrow above section headings.
- [ ] Layout is not a centered stack of equal cards unless the content is
      genuinely a set of peers.
- [ ] Type scale has at least three deliberate steps; line-height varies with size.
- [ ] Body measure ≤ 68ch; data uses tabular numerals.
- [ ] Every interactive element has a visible hover and focus state.
- [ ] Text contrast ≥ 4.5:1, including muted text and placeholder text.
- [ ] Borders use a token colour, not `border-gray-200` repeated ad hoc.
- [ ] Works at 320px wide and at 1440px without reflow bugs.

## Grep pass

Before opening a PR that touches UI, search for the tropes:

```bash
rg -n "bg-gradient|from-(indigo|violet|purple|cyan)-[0-9]|to-(indigo|violet|purple|cyan)-[0-9]" .
rg -n "bg-clip-text|text-transparent" .
rg -n "shadow-(xl|2xl)|blur-3xl|backdrop-blur" .
rg -n "rounded-full" .
rg -n "font-family:\s*(Inter|Roboto)" .
rg -n "linear-gradient|radial-gradient" web/templates
rg -n "#(6d28d9|7c3aed|8b5cf6|6366f1|4f46e5|06b6d4|22d3ee)" web/templates
```

Each hit needs a justification in the diff or a replacement. Do not suppress
the check — fix the component.

## Edge cases

- **A gradient IS the product** (image tooling, colour pickers, brand-prescribed
  marketing assets): follow the brand, and keep the gradient out of the
  functional UI chrome.
- **Dark mode:** define both surfaces from tokens; never reuse a light border
  colour on a dark background. Contrast is checked per mode.
- **shadcn/ui, Tailwind presets, component kits:** they ship slop defaults
  (rounded-2xl cards, `muted` greys, default Inter). Restyle the tokens first,
  then compose.
- **Existing legacy UI:** match the surrounding system rather than upgrading one
  component in isolation; consistency beats a locally better component.
