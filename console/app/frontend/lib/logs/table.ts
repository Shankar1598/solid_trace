// A tabular Query's result: the table shown in place of the stream when a
// Query ends in summarize, project, distinct, count, top and the like.

import { valueText } from '../charts/axis'

export type ColumnType = 'string' | 'number' | 'bool' | 'datetime' | 'timespan' | 'dynamic'

/** The aggregate a column holds, when it is one of the last summarize's, kept through a plain rename. */
export type Agg = 'count' | 'countif' | 'dcount' | 'sum' | 'avg' | 'min' | 'max' | 'percentile'

export interface TableColumn {
  name: string
  type: ColumnType
  agg?: Agg
  /** On the last summarize's time bin column, bin(time, X) or bin_auto(time): the bin's size. */
  bin_seconds?: number
}

/** A table as EventStore answers it; truncated says rows were cut at 10,000. */
export interface LogTable {
  columns: TableColumn[]
  /** One value per column: datetimes as RFC 3339 UTC, dynamic as the JSON value held. */
  rows: unknown[][]
  truncated: boolean
  /**
   * True when a bin with no row has no Logs: nothing filters or cuts the
   * rows after the last summarize, so a chart may draw its empty bins.
   */
  fill_gaps?: boolean
  /** The range the Query ran over, RFC 3339 UTC, narrowed by a leading where on time. */
  from?: string
  to?: string
  /**
   * A time chart's totals, when the run asked for them and the Query ends in
   * a summarize by a time bin: a row per group (the other by columns) with
   * its aggregates over the whole range, largest first.
   */
  totals?: LogTotals
}

/** A time chart's totals over the range; truncated says rows were cut at 10,000. */
export interface LogTotals {
  columns: TableColumn[]
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

/**
 * A number as a table cell shows it: an aggregate read as a chart reads it
 * (4,962 or 544.24), any other number exactly as the Log holds it, since it
 * may be an ID or a status.
 */
export function numberText(column: TableColumn, value: number): string {
  return column.agg ? valueText(value) : String(value)
}
