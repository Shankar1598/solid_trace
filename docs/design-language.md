# The Console's design language

The Console uses one design language, chosen in [ticket 20](../.scratch/logs-v2/issues/20-looks-pass.md) as direction B: sans for the interface, mono for the data. Follow it in every new page and component. The tokens live in `console/app/frontend/css/globals.css`.

## Type

- **IBM Plex Sans** (`font-sans`, the default) is for the interface: titles, navigation, buttons, labels, help text and prose.
- **JetBrains Mono** (`font-mono`) is for data: timestamps, IDs, values, Log messages, Queries, field names, stack frames, DSNs, slugs, chart axes and numbers in tables. The font change tells a reader which text is data.
- Mono's ligatures are off (`font-variant-ligatures: no-contextual` on `html`), so `==` and `!=` look the way they are typed.
- Sentence case everywhere: "Save changes", "New monitor", "Last seen". UPPERCASE only in `caps-label`, the small section label.

## Shape and colour

- **Square corners.** Every `--radius*` token is zero. Only status dots and avatars stay round (`rounded-full`). Don't add `rounded-*` classes.
- **Neutral by default.** Primary buttons are ink on paper. The brand mark is monochrome.
- **Colour only for meaning:**
  - `info` is blue.
  - `warning` is amber.
  - `destructive` is red, for errors.
  - `success` is green, for OK and resolved.

  Use these tokens, not Tailwind hues such as `text-amber-600`. Charts keep their own series palette. Log levels keep their squares and text colours (`LEVEL_TEXT`, `LEVEL_SQUARE`).
- **Panels are outlined, not filled.** The page is the background; a panel is a border on it. Fill is for what floats (menus, popovers, the inspector) and for code.

## Components

- **`PageHeader`** opens every signed-in page:
  - breadcrumbs on nested pages
  - the title, which also names the browser tab
  - a status `Badge` beside the title
  - one line of description
  - the page's buttons on the right

  Logs is the exception: its explorer fills the page, so it has a screen-reader `h1` and a tab title, and its bar stands in for the header.
- **`Table`** for every list: sentence-case grey headers with no fill, and numbers right-aligned in mono with tabular figures. Format numbers for display with `valueText` or `numberText`, with full precision in a `title`.
- **`Badge`** tones:
  - `neutral` for labels such as `error` or `staging`
  - `info`, `warn` and `error`
  - `ok`
  - `count` for a number beside a tab

  Pass `dot` when the badge is a status.
- **`Segmented`** for a switch between a few modes (Builder or Query, chart kind, Monitor kind). It is as tall as an input, and its chosen option is in ink.
- **`FormSection`** for each part of a long form: a title, one line about it, then its fields, with a rule above. Forms are at most `max-w-4xl` wide, with Cancel then the primary button at the end, on the right.
- **`Card`** for a short form or a group of settings: `CardTitle` and `CardDescription`, then the fields, then a footer with the button.
- **`Skeleton`** while something loads, shaped like what's coming (`ChartSkeleton`, the stream's `StreamSkeleton`), inside a `role="status"` that names it.

## Access

- Every icon-only button has an `aria-label` (and a `title` for the mouse).
- Toggles say their state with `aria-pressed`.
- A list that opens a detail on click also opens it from the keyboard. The Logs stream, for example, is a grid with one tab stop: the arrow keys move between rows, and Enter opens one.
