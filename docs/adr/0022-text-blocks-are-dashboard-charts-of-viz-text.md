# Text blocks are Dashboard Charts of viz text

A **Text block** is stored as a row of `dashboard_charts` with viz `text`. The row keeps the Markdown in a `text` column, and its `project_id` and `query` are null; its title is optional. A Chart has `text` null. So a Text block and a Chart share one grid, one layout endpoint, the duplicate and delete endpoints, the **Sections**, and the rules that give an item its place (a new item goes under the others of its Section, and a move saves only the places that changed). Over MCP, `add_chart` with `viz: "text"` adds a Text block, and `get_dashboard`, `update_chart`, `delete_chart` and `get_chart_data` take it as they take a Chart.

A future reader may ask why a Text block is a "chart" in the table, the routes and the MCP tools. This decision is the cause.

We chose this because a Text block is a card on the grid like a Chart. A member moves, resizes, duplicates and deletes it as a Chart, and it is in a Section or in none as a Chart is. One table keeps one list of places for each Section, so the grid code and the place rules stay in one place.

## Considered options

- **A separate model and table for Text blocks.** The names would be correct, and no Chart code would have to check for a Text block. But the grid would then mix two lists with one set of places: the layout endpoint, the place rules, the move into a Section, the delete of a Section and the duplicate would each read and write two tables, and keep them in step. The MCP tools would need a second set of tools, or tools that take two kinds of id. That is more code than the checks below.

## Consequences

- The model, the controller, the MCP tools and the page check for a Text block: `DashboardChart#text?` in Ruby and `isTextBlock` in the page. A Text block needs no Project, no Query and no title; it has no data to run; it is not checked through validate; and the page draws it with its own card and editor. A new feature for Charts must decide what it does with a Text block.
- In code and in docs the thing is a "Text block", never a "text chart".
- A Chart does not change into a Text block, nor a Text block into a Chart.
- It is hard to reverse. The Text blocks are stored rows of `dashboard_charts`, so a separate table would need a data migration. MCP clients already add and read Text blocks as `viz: "text"` through the Chart tools, so a change breaks that contract for them.
