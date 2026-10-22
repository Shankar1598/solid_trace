# Dashboard filters are Query parameters that EventStore fills in

A Dashboard's filters reach its Charts through the Query language. A Chart uses a filter by naming it in its Query, as in `where service == $service`. The Console sends the values of the filters a Chart names with its run, and EventStore fills them in when it compiles the Query. A filter set to All makes its comparison true, so it drops out. A filter applies only to the Charts that name it, and one filter can be compared with differently named fields in different Projects.

A future reader may wonder why `$` changed meaning in the lexer, and why `where service == $service` matches every Log when nothing is chosen. Both come from this decision.

## Considered options

- **A tick box per Chart, with the field bound to the filter, and Console adding `where field == value` before the Query:** needs no Query edits. But the field must have the same name in every Chart's Project, and the filter always applies first, never after an `extend`.
- **Console splices the values into the Query text:** EventStore is untouched. But the browser and Console never parse Queries, so errors can't point at the filter, the Builder can't read `service == $service` back as a chip, and All can't remove a comparison without a parser.
- **Axiom's form, `declare query_parameters (f:string = "")` with `isempty(f) or field == f`:** nothing implicit, but every Chart writes the All case by hand.

## Consequences

- `$` no longer starts a field name. A field named `$type` is written `['$type']`.
- A filter's value is text. Compared with an Attribute, it matches the text, or the number or bool the text reads as.
- The values become literals in the compiled SQL, so the bin cache keeps each set of values apart without a change. All compiles to the SQL of the Query without the comparison, and shares its cached bins.
- A name the request doesn't fill is All. So the explorer, the MCP server's `query_logs` and anything else that sends no values runs every filter as All. Monitors refuse Queries that name one.
