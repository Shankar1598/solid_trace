# Dashboard filters

**Status:** ready-for-agent

Planned with the user on 2026-10-21, like Axiom's dashboard filters. Tickets: [01](issues/01-filters-in-the-query-language.md), [02](issues/02-dashboard-filter-bar.md), [03](issues/03-select-of-a-fields-values.md), [04](issues/04-filters-in-the-builder.md), [05](issues/05-mcp-tools-and-monitors.md). Decided in [ADR 0014](../../docs/adr/0014-dashboard-filters-are-query-parameters.md).

## Problem Statement

A Dashboard shows its Charts over one time range, and nothing else about what they show can change while you look. To see the same Dashboard for one service, one environment or one customer, you edit every Chart's Query by hand, or keep a copy of the Dashboard per service. During an incident that is too slow: you want the "API" view of the Dashboard you already know, now, and a link to it for the person you're working with.

Axiom solves this with a filter bar: one set of controls at the top of a Dashboard, whose values the Charts that ask for them use. SolidTrace has no such thing. A Chart's data request carries only the range, and a Chart is its Query and a Project.

## Solution

A Dashboard gets a filter bar under its header. Each Dashboard filter is a select (from a fixed list, or from a field's most common values in a Project, one value or several) or a search box. A Chart uses a filter by naming it in its Query, as `$name`: `where service == $service`, or `search $q`. Picking "api" in the `service` filter re-runs every Chart whose Query names `$service`, with that value filled in, and leaves the other Charts alone. When a filter is All, which is how every Dashboard opens, comparisons that name it are dropped, so a Chart reads as it did before the filter existed.

The values live in the URL, so a link shares exactly what you see. In the Chart editor's Builder, a chip's value can be a filter instead of a typed value. The MCP server can make, read and use filters too.

## User Stories

1. As a Dashboard viewer, I want a bar of filters at the top of a Dashboard, so that I can narrow its Charts without editing them.
2. As a Dashboard viewer, I want every filter to start as All, so that opening a Dashboard shows everything, as it does today.
3. As a Dashboard viewer, I want to pick one value in a select filter, so that the Charts that use it show only that value's Logs.
4. As a Dashboard viewer, I want to pick several values in a multiple select, so that I can compare, say, two services together.
5. As a Dashboard viewer, I want to type into a search filter and press Enter, so that the Charts that use it show only Logs matching my text.
6. As a Dashboard viewer, I want to clear a search filter, so that it goes back to All.
7. As a Dashboard viewer, I want All at the top of every select, so that I can drop a filter in one click.
8. As a Dashboard viewer, I want the chosen values in the URL, so that I can send a colleague a link to exactly what I see.
9. As a Dashboard viewer, I want a reload to keep my filter values, so that I don't lose my place.
10. As a Dashboard viewer, I want the range picker and the filters to work together, so that changing one keeps the other.
11. As a Dashboard viewer, I want changing a filter to re-run only the Charts that use it, so that the Dashboard answers quickly and the other Charts don't flicker.
12. As a Dashboard viewer, I want auto-refresh to keep using the chosen values, so that a live Dashboard stays filtered.
13. As a Dashboard viewer, I want a multiple select to say which values it holds, such as "service: api, web" or "service: 3 values", so that I can tell what's applied at a glance.
14. As a Dashboard viewer, I want a select of a field's values to list that field's most common values over the range I'm looking at, so that I pick from values that actually occur.
15. As a Dashboard viewer, I want to narrow a long list of values by typing, so that I can find one among a hundred.
16. As a Dashboard viewer, I want a value from a link to show as chosen even when it isn't among today's options, so that a shared link still reads correctly.
17. As a Dashboard viewer, I want a Chart that names a filter the Dashboard lacks to say so, so that I know why it isn't following the bar.
18. As a Dashboard maker, I want to add a filter from a "Filters" button, so that I can set up the bar without leaving the Dashboard.
19. As a Dashboard maker, I want to give each filter a name for Queries and a label for the bar, so that Queries stay short and the bar reads well.
20. As a Dashboard maker, I want to choose a filter's kind (select or search) and, for a select, whether it takes several values, so that each filter fits its field.
21. As a Dashboard maker, I want to type a select's options as labels and values, so that I can offer a short fixed list such as error, warn and info.
22. As a Dashboard maker, I want a select's options to come from a field in one of the Organization's Projects, so that I don't keep a list of services up to date by hand.
23. As a Dashboard maker, I want to reorder filters, so that the most used comes first.
24. As a Dashboard maker, I want to remove a filter, and be told first which Charts name it, so that I don't silently change Charts.
25. As a Dashboard maker, I want a saved filter's name to stay fixed, so that the Charts that name it keep working.
26. As a Dashboard maker, I want to write `where service == $service` in a Chart's Query, so that the Chart follows the `service` filter.
27. As a Dashboard maker, I want one filter to apply to differently named fields in different Projects, such as `service` in one and `app` in another, so that one bar serves a mixed Dashboard.
28. As a Dashboard maker, I want to use a filter after an `extend` or a `summarize`, so that it can narrow a computed column.
29. As a Dashboard maker, I want `service != $service` to keep Logs whose service is none of the chosen values, so that I can build an "everything but" Chart.
30. As a Dashboard maker, I want text operators such as `contains` and `startswith` to take a filter, so that a search-like filter can match part of a field.
31. As a Dashboard maker, I want a filter compared with an Attribute to match `500` whether senders sent the number or the string, so that a status filter just works.
32. As a Dashboard maker, I want a clear error at the `$name` when I put a filter where it can't go, so that I know to move it.
33. As a Dashboard maker, I want the Query editor to colour `$name`, so that filters stand out from fields.
34. As a Dashboard maker, I want a Builder chip's value to be one of the Dashboard's filters, so that I can make a filtered Chart without typing a Query.
35. As a Dashboard maker, I want the Builder's search box to take a search filter, so that a search Chart follows the bar.
36. As a Dashboard maker, I want a filter chip to look different from a typed value, so that I can tell `level == $level` from `level == "error"`.
37. As a Dashboard maker, I want the Chart editor's preview to use the bar's current values, so that I see what the Chart will show.
38. As a Dashboard maker, I want the Chart editor to refuse a Query naming a filter the Dashboard lacks, so that I catch typos before saving.
39. As a Dashboard maker, I want a filter set to All to leave a Chart exactly as fast as before, so that adding filters costs nothing until someone uses them.
40. As an Organization member, I want any member to be able to change a Dashboard's filters, so that filters follow the same rule as the Dashboard itself.
41. As an engineer in the Logs explorer, I want a Query copied from a Chart to run with its filters as All, so that it works there without a filter bar.
42. As a Monitor maker, I want a Monitor's Query naming a filter to be refused with a clear message, so that a Monitor never fires on a value nobody chose.
43. As an AI agent on the MCP server, I want `get_dashboard` to list the Dashboard's filters, so that I know which values I can pass.
44. As an AI agent on the MCP server, I want to make and change a Dashboard's filters with the Dashboard tools, so that I can set up a Dashboard completely.
45. As an AI agent on the MCP server, I want `get_chart_data` to take filter values, so that I can read a Chart for one service.
46. As an AI agent on the MCP server, I want adding or changing a Chart that names an unknown filter to be refused, naming the Dashboard's filters, so that I can correct it.
47. As an AI agent on the MCP server, I want the Query language reference to explain `$name`, so that I write filtered Queries correctly.
48. As an operator, I want filter values bounded in number and size, so that a crafted URL can't make a huge Query.
49. As a Dashboard viewer, I want a field-values select to load its options only when I open it, so that a Dashboard with filters loads as fast as one without.
50. As a Dashboard viewer, I want a field-values select that failed to load to show the error, so that I know why it's empty.

## Implementation Decisions

### A filter

- A Dashboard's filters are an ordered list, the order the bar shows them. Each has:
  - **name**: what a Query writes after `$`. Lowercase letters, digits and `_`, starting with a letter or `_`, at most 40 characters, unique on the Dashboard. It can't change once saved; to rename, add a new filter and remove the old one.
  - **label**: what the bar shows, at most 100 characters; the name when empty.
  - **kind**: `select` or `search`.
  - **multiple** (selects only): whether it takes several values.
  - **options** (selects only): either a fixed list of up to 200 `{label, value}` pairs (strings of at most 200 characters, values unique and not empty), or a field and a Project of the Dashboard's Organization (a field name of at most 200 characters, not `time`).
- A Dashboard has at most 20 filters.
- **Schema change**: one JSON column on dashboards, `filters`, default an empty list, not null. Not a table: the filters are edited, saved and read as one list with their Dashboard. The Dashboard model validates the list, naming the filter in each error (as `filters[1].name`).
- Any member can change the filters, as any member can change a Dashboard. The Dashboard's update takes the whole list.

### In a Query

- `$name` stands for a filter's value. It may be compared on either side with `==`, `!=`, `=~`, `!~`, and the text operators except `matches regex` (`contains`, `!contains`, `startswith`, `!startswith` and their case-sensitive forms). It may also be searched: `search $q`.
- **One value** acts as if it were written there as a string.
- **Several values**: a positive operator matches any of them, a negated one none of them.
- **All** (no value, or the empty string): the comparison is true, as if it weren't there, and `search $q` keeps every Log. All compiles to the same SQL as the Query without that comparison.
- **Types**: a value is text. Against a built-in field, a string or a text operator, it is text. Against an Attribute, it matches the text, or the number or bool the text reads as. So `==` becomes an "or" of the two, and `!=` an "and". Against a number column, after `extend` or `summarize`, it is a number, and a value that isn't one matches nothing.
- **Errors**: `$name` anywhere else is an error at its span, saying where filters go. That means `project`, `extend`, `summarize`, `sort`, `in (…)` lists, ordered comparisons, `matches regex`, and inside `not(…)`, whose All would flip to "nothing"; `!=` is the way to exclude.
- **Lexer change**: `$` no longer starts a field name. A field named `$type` is written `['$type']`. `$` stays allowed inside a name.
- **Unknown names are All.** EventStore never sees a Dashboard. A name the Query uses that the request doesn't fill is All, so the explorer, the MCP server's `query_logs` and anything else that sends no values runs every filter as All.

### EventStore's query API

- The query request takes `parameters`: an object of filter name to a list of strings. Absent or an empty list means All. More than 100 values for one name, or a value over 1,000 bytes, is a 400.
- EventStore fills the values in when it compiles, not Console when it builds the text. One rule then serves Charts, the chart editor's preview and the MCP server, errors point at `$name` in the text, and validate reads filters back as Builder chips.
- The compiler takes the values as an input beside the range. The compiled Query reports the names it uses, in order of first use.
- The validate response adds `parameters`, the names the Query uses. Its chips carry a filter value as `{"filter": "<name>"}`, and a `search $q` stage as `search_filter: "<name>"` with `search` null.
- Histogram and readable-SQL requests compile with no values (All).
- **Bin cache**: unchanged. Values become literals in the compiled SQL, which the bin key already hashes, so each set of values gets its own bins, and All shares the bins of the Query without the comparison.

### Console

- The Dashboard's page props carry the filters.
- **URL contract**: values sit beside the range as `v_<name>`, as Axiom's `v_FILTER_ID`, and several values repeat the key: `?range=1h&v_service=api&v_service=web&v_q=timeout`. A filter missing from the URL is All. The bar writes the URL with a replace, as the range picker does, and redirects after saving a Chart or the Dashboard keep the `v_` values.
- **A Chart's data request** carries the `v_` values of the filters its Query names. The browser finds those names with its Query lexer, which gains a parameter token, since the browser never parses Queries. A Chart re-runs when one of its own filters' values changes, and not otherwise. Console reads repeated keys as lists and passes them to EventStore as `parameters`, through the existing chart-run path and its cap on concurrent runs.
- The chart editor's preview sends the bar's current values as `parameters` in its body.
- **Field values endpoint**: `GET` a Dashboard filter's values, with `from` and `to`. It answers `{values: [{value, count}]}`, the field's 100 most common values in the filter's Project over the range, most common first. It runs as a chart run, within the same cap, as the Query `['<slug>'] | where isnotnull(['<field>']) | summarize count() by ['<field>'] | sort by count_ desc | take 100`, with the slug and field escaped in their brackets. A value is its text: a number as written, a bool as `true` or `false`. The dropdown calls it when opened, and again when opened after the range changed.
- **Filter bar and dialog**: the bar sits under the Dashboard's header, with a "Filters" button that opens a dialog to add, remove, reorder and edit filters. A saved filter's name is shown read-only. Removing a filter that Charts name asks first, listing them. Afterwards those Charts run it as All, and their cards say the name isn't one of the Dashboard's filters.
- **Builder**: a chip's value can be a filter, written `$name`, offered for `==`, `!=` and the text operators only. The search box can be a search filter, written `search $q`. The Builder panel takes an optional list of the Dashboard's filters. The Chart editor passes it; the explorer passes none and is unchanged. A filter chip is styled apart from a typed value.
- The Chart editor shows a problem at any name in validate's `parameters` that isn't one of the Dashboard's filters, and won't save it. Charts saved another way, with an unknown name, run it as All.
- **Monitors**: the Monitor Query check refuses a Query that names a filter: "Filters ($service) work only on Dashboard Charts". This covers the Monitor form and the MCP Monitor tools.

### MCP server

- `get_dashboard` lists the filters: name, label, kind, multiple and options.
- `create_dashboard` and `update_dashboard` take `filters`, the whole list, validated as the Console's are.
- `get_chart_data` takes `filters`, an object of name to a string or a list of strings, and its answer says which values it ran with.
- `add_chart` and `update_chart` refuse a Query naming a filter the Dashboard lacks, using validate's `parameters`, and list the Dashboard's filters.
- The Query language reference, which the MCP server serves, gains a "Dashboard filters" section.

### Docs and domain

- GLOSSARY.md gains **Dashboard filter**. ADR 0014 records why filters are Query parameters that EventStore fills in.

## Testing Decisions

- A good test drives the feature through a public interface and checks what a user or caller sees: rows, counts, errors and their positions, the URL, which requests went out. It doesn't check internals such as the compiled AST or a component's state, so the tests survive refactoring.
- **Seam 1, EventStore's query API** (Go, root package, `go test -p 1` with `-run` for these tests): seed known Logs, then call the query and validate endpoints with `parameters`. Cover:
  - one value, several with `==` (any) and `!=` (none), and text operators;
  - All gives the same rows as the Query without the comparison;
  - an Attribute holding `500` in one Log and `"500"` in another matches both;
  - a built-in field compares as text, and a number column after `summarize` as a number;
  - each error case, with its position;
  - `['$type']` still reads a field;
  - validate's `parameters`, its filter chips and `search_filter`;
  - the 400s for too many or too long values;
  - two values of a binned Chart don't share bin-cache bins, and All shares the bins of the Query without the comparison.
  Prior art: the root package's logs language, query and bin-cache tests, which seed Logs into a test EventStore and query it.
- **Seam 2, Console's endpoints and MCP tools** (Rails, EventStore stubbed). Cover:
  - the Dashboard's filter validations, through its update;
  - the Chart data and preview endpoints passing `parameters`, with repeated `v_` keys as lists;
  - redirects keeping `v_` values;
  - the field-values endpoint's Query text (escaping included), its answer, and another Organization's Project refused;
  - each MCP Dashboard tool's filter behaviour;
  - the Monitor check refusing filters, and `query_logs` sending `parameters`.
  Prior art: the dashboards controller tests, the Dashboard MCP tools tests, and the Monitor query check tests.
- **Seam 3, the Dashboard page in a browser** (one Playwright flow on the dev stack):
  - add a fixed-list `level` filter and a Chart `where level == $level | summarize count()`;
  - picking `error` changes the number and puts `v_level=error` in the URL, and a reload keeps it;
  - a Chart that doesn't name `$level` isn't fetched again;
  - All brings the first number back;
  - a multiple select, and a search filter with `search $q`;
  - a field-values filter on the dev Project's `service`;
  - a Builder chip `level == $level`;
  - a Query naming `$nope` can't be saved.
  Prior art: the dashboards flow and the Builder flows.
- **vitest only for the browser's pure helpers**: finding `$names` with the lexer (not inside strings or `['…']`), and reading and writing `v_` values beside the range. Prior art: the Dashboard URL helper tests and the Query lexer tests.
- Full Go, Rails and Playwright suites are for the user, as usual; implementers run the targeted tests above.

## Out of Scope

- **SQL Charts on Dashboards**, and `$name` in SQL. Dashboards can't hold SQL Charts yet, so that is its own effort, filed as [dashboard-sql-charts 01](../dashboard-sql-charts/issues/01-sql-charts-on-dashboards.md) (the user, 2026-10-21).
- Dependent filters, such as a city filter that lists only the chosen country's cities.
- Options from a Query, as Axiom's APL options query.
- Saved default values: every filter opens as All (the user, 2026-10-21).
- The explorer applying a Dashboard's filter values. "Open in explorer" shows the Chart's Query with its filters All.
- Clicking a value in a Chart to set a filter.
- Filters in Monitors.

## Further Notes

- **Choices the user made on 2026-10-21:**
  - Charts opt in by `$name` in the Query, not by a tick box per Chart.
  - Every kind is in: a select from a fixed list, from a field's values, multiple selects, and search boxes.
  - Values live in the URL with All as the default.
  - The MCP tools are in this effort.
  - SQL Charts are later, in their own effort.
- **Rejected alternatives**, in ADR 0014:
  - a tick box per Chart with the field bound to the filter, which needs the same field name in every Project and always filters first;
  - Console splicing values into the Query text, which can't place errors, read chips or drop a comparison without a parser;
  - Axiom's `declare query_parameters` with a hand-written `isempty()` All case.
- **Order**: 01 first. Then 02, then 03 and 04 (independent of each other), then 05. The work happens on `feature/dashboard-filters`, in the worktree `../solid_trace-dashboard-filters`, branched from `main` at `e4ba288`.

## Comments
