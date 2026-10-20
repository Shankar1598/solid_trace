# Dashboard filters

Planned with the user on 2026-10-21: a filter bar at the top of a Dashboard, as in Axiom. A Chart uses a filter by naming it in its Query, as `$name`, so a filter narrows only the Charts that use it. Picking "api" in a `service` filter re-runs every Chart whose Query says `service == $service`, and leaves the others alone.

Today a Dashboard has nothing like this: every Chart's data request carries only `from` and `to`, and a Chart is its Query text and a Project.

## What we're building

1. **Filters in the Query language** ([01](issues/01-filters-in-the-query-language.md)): `$name` in a Query stands for a filter's value. EventStore fills the values in when it compiles the Query; a filter set to All drops out.
2. **A Dashboard's filter bar** ([02](issues/02-dashboard-filter-bar.md)): filters saved with the Dashboard, a bar of them under its header, their values in the URL, and each Chart run with the values of the filters it names. Selects from a fixed list, and search boxes; a select can take several values.
3. **Selects of a field's values** ([03](issues/03-select-of-a-fields-values.md)): a select whose options are a field's most common values in a Project over the range shown.
4. **Filters in the Chart editor's Builder** ([04](issues/04-filters-in-the-builder.md)): a chip's value, or the search box, can be a filter.
5. **MCP tools, and Monitors** ([05](issues/05-mcp-tools-and-monitors.md)): the Dashboard tools make and read filters, `get_chart_data` takes their values, and a Monitor's Query can't name one.

## How it works

### A filter

A Dashboard's filters are a list, in the order the bar shows them. Each has:

- **name**: what a Query writes after `$`. Lowercase letters, digits and `_`, starting with a letter or `_`, at most 40 characters, unique on the Dashboard. It can't change once saved, since Charts name it; to rename, add a new filter and remove the old one.
- **label**: what the bar shows; the name when left empty.
- **kind**: `select` or `search`.
- **multiple** (selects only): whether it takes several values.
- **options** (selects only): a fixed list of `{label, value}`, or a field and a Project, whose most common values over the range shown are the options (03).

### In a Query

- `$name` may be compared with `==`, `!=`, `=~`, `!~`, and the text operators but `matches regex` (`contains`, `!contains`, `startswith`, `!startswith` and their `_cs` forms), on either side: `where service == $service`. And it may be searched: `search $q`.
- **One value** acts as if it were written there as a string.
- **Several values** (a select with multiple): a positive operator matches any of them, a negated one none of them. `service != $service` keeps Logs whose service is none of the chosen.
- **All** (no value): the comparison is true, as if it weren't there, and `search $q` keeps every Log. An empty string is All too, so a filter can't pick the empty string.
- **Types**: a value is text. Compared with a built-in field, a string, or by a text operator, it is text. Compared with an Attribute, it matches the text, or the number or bool the text reads as: `http.status == $status` set to `500` matches both `500` and `"500"`. Compared with a number column (after `extend` or `summarize`), it is a number, and a value that isn't one matches nothing.
- **Errors**: `$name` anywhere else is an error at it, saying where filters go: in `project`, `extend`, `summarize`, `sort`, `in (…)` lists, `<`, `<=`, `>`, `>=`, `matches regex`, and inside `not(…)` (whose All would flip to nothing; use `!=`).
- A field whose name starts with `$` is written `['$type']`.
- **Unknown names are All.** EventStore never sees a Dashboard: the request carries `parameters: {name: [values]}`, and a name the Query uses but the request lacks is All. So the explorer, which sends none, runs every filter as All.

### Values in the URL

`?range=1h&v_service=api&v_service=web&v_q=timeout`, as Axiom's `v_FILTER_ID`. Several values repeat the key. A filter missing from the URL is All, and opening a Dashboard from the sidebar shows every filter as All. Nothing is saved as a default.

A Chart sends only the values of the filters its Query names, found with the browser's Query lexer, so changing a filter re-runs only the Charts that use it.

### The bar

Under the Dashboard's header, beside nothing when there are no filters but a "Filters" button. A select is a dropdown with All first; a multiple select ticks values and reads "service: api, web" or "service: 3 values". A search box applies on Enter and clears to All. The bar edits the URL with `router.replace`, as the range picker does.

### Editing filters

"Filters" opens a dialog listing the filters: add, remove, reorder, and change each one's label, kind, multiple and options. Saving sends the whole list through the Dashboard's update. Removing a filter that Charts name asks first, naming those Charts. They then run it as All, and their card says `$service` isn't one of the Dashboard's filters. The Chart editor marks such a name as a problem and won't save it.

## Decisions

1. **A Chart opts in by naming the filter in its Query** (the user, 2026-10-21), as in Axiom. One filter can be compared with differently named fields in different Projects' Charts, and can sit after an `extend` or `summarize`. Rejected: a tick box per Chart with the field bound to the filter, which needs the same field name in every Project and always filters first.
2. **All drops the comparison by itself.** Axiom has each Query declare its filters (`declare query_parameters (f:string = "")`) and write the All case (`isempty(f) or field == f`). Here `service == $service` is enough.
3. **EventStore fills in the values when it compiles**, rather than Console splicing them into the text. One rule serves Charts, the chart editor's preview and the MCP server. Errors point at the `$name` in the text, and validate can read `service == $service` back as a Builder chip. Values become literals in the SQL, so the bin cache keeps each set of values apart without a change. And All compiles to the same SQL as the Query without the comparison, so it shares that Query's cached bins.
4. **An Attribute matches the value as text or as the number or bool it reads as.** Filter values come from URLs and dropdowns as text, while an Attribute is stored as whatever type each sender gave it.
5. **Values live in the URL only, All by default** (the user). A link shares what you see. There is no saved default value.
6. **The filters are a JSON list on the Dashboard** (`dashboards.filters`), not a table: they are edited, saved and read as one list with their Dashboard. Any member can change them, as any member can change a Dashboard.

## Out of scope

- **SQL Charts on Dashboards**, and `$name` in SQL: Dashboards can't hold SQL Charts yet. Their own effort, filed as [dashboard-sql-charts 01](../dashboard-sql-charts/issues/01-sql-charts-on-dashboards.md) (the user, 2026-10-21).
- Dependent filters, such as a city filter that lists only the chosen country's cities.
- Options from a Query, as Axiom's APL options query.
- Saved default values.
- The explorer applying a Dashboard's filter values: "Open in explorer" shows the Chart's Query with its filters All.
- Clicking a value in a Chart to set a filter (`Show.tsx`'s `onFilter={null}`).
- Filters in Monitors.

## Comments
