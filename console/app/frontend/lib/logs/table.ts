// A tabular Query's result: the table shown in place of the stream when a
// Query ends in summarize, project, distinct, count, top and the like.

export type ColumnType = 'string' | 'number' | 'bool' | 'datetime' | 'timespan' | 'dynamic'

export interface TableColumn {
  name: string
  type: ColumnType
}

/** A table as EventStore answers it; truncated says rows were cut at 10,000. */
export interface LogTable {
  columns: TableColumn[]
  /** One value per column: datetimes as RFC 3339 UTC, dynamic as the JSON value held. */
  rows: unknown[][]
  truncated: boolean
}

/**
 * Whether a column's cells offer Show only this and Hide this. The pivot is a
 * where on the column's name over the Logs feeding the table, so it fits
 * columns that are Fields: a by, project or distinct column (a string, bool
 * or dynamic value). Numbers are mostly aggregates no Log has, and times are
 * filtered by the time range.
 */
export function pivotsOn(column: TableColumn): boolean {
  return column.type === 'string' || column.type === 'bool' || column.type === 'dynamic'
}
