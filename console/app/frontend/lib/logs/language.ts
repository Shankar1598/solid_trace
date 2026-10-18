// The Query language as the editor knows it, for highlighting and
// completions: its stages, functions, operators and keywords, each with a
// one-line signature. docs/logs/query-language.md is the reference; EventStore
// is the judge (validate reports what it can't read).

/** A stage, offered at the start of a Query and after each `|`. */
export interface StageInfo {
  /** As written, such as `where` or `sort by`. */
  name: string
  signature: string
  /** What it does, in a line. */
  info: string
  /** Another name for a stage listed before it, offered after it. */
  alias?: boolean
}

export const STAGES: StageInfo[] = [
  { name: 'where', signature: 'where condition', info: 'Keeps the rows the condition is true for.' },
  { name: 'search', signature: 'search "text"', info: 'Keeps the Logs whose message or string Attributes contain the text.' },
  { name: 'summarize', signature: 'summarize agg, … by expr, …', info: 'One row per distinct by value, with the aggregates.' },
  { name: 'project', signature: 'project a, b, name = expr', info: 'Keeps only these columns.' },
  { name: 'extend', signature: 'extend name = expr, …', info: 'Adds columns to each row.' },
  { name: 'sort by', signature: 'sort by expr [asc|desc], …', info: 'Sorts the rows, descending unless asc.' },
  { name: 'take', signature: 'take n', info: 'Keeps the first n rows; for Logs, the newest n.' },
  { name: 'top', signature: 'top n by expr [asc|desc]', info: 'Sorts the rows and keeps the first n.' },
  { name: 'count', signature: 'count', info: 'One row with one column, Count: the number of rows.' },
  { name: 'distinct', signature: 'distinct a, b', info: 'The distinct combinations of the columns.' },
  { name: 'order by', signature: 'order by expr [asc|desc], …', info: 'The same as sort by.', alias: true },
  { name: 'limit', signature: 'limit n', info: 'The same as take.', alias: true },
]

/** The words that open a stage. */
export const STAGE_WORDS = new Set(STAGES.map(s => s.name.split(' ')[0]))

/** A function, with its arguments as a reader writes them. */
export interface FunctionInfo {
  name: string
  /** Between the parentheses, such as `x, size`; empty for none. */
  args: string
  info: string
  /** What picking it writes, when more than its parentheses; `|` marks where the cursor lands. */
  write?: string
}

/** Functions of one row, for any expression. */
export const SCALAR_FUNCTIONS: FunctionInfo[] = [
  { name: 'bin', args: 'x, size', info: 'x rounded down to a multiple of size, as bin(time, 5m).', write: 'bin(time, |)' },
  { name: 'bin_auto', args: 'time', info: 'time rounded down to a bin size the time range picks.', write: 'bin_auto(time)|' },
  { name: 'ago', args: 'timespan', info: 'The time a timespan before the run, as ago(1h).' },
  { name: 'now', args: '', info: 'The time of the run.' },
  { name: 'tostring', args: 'x', info: 'x as a string.' },
  { name: 'toint', args: 'x', info: 'x as a whole number, rounded toward zero.' },
  { name: 'tolong', args: 'x', info: 'x as a whole number, rounded toward zero.' },
  { name: 'todouble', args: 'x', info: 'x as a number.' },
  { name: 'toreal', args: 'x', info: 'x as a number.' },
  { name: 'tolower', args: 's', info: 'A string in lower case.' },
  { name: 'toupper', args: 's', info: 'A string in upper case.' },
  { name: 'strlen', args: 's', info: 'The length of a string.' },
  { name: 'isempty', args: 'x', info: 'Whether x is missing or an empty string.' },
  { name: 'isnotempty', args: 'x', info: 'Whether x is there and not an empty string.' },
  { name: 'isnull', args: 'x', info: 'Whether x is missing or an empty string.' },
  { name: 'isnotnull', args: 'x', info: 'Whether x is there and not an empty string.' },
  { name: 'not', args: 'condition', info: 'The condition negated.' },
  { name: 'floor', args: 'x, size', info: 'The same as bin.' },
  { name: 'datetime', args: '2026-10-02T10:00:00Z', info: 'A point in time; without a zone it is UTC.' },
]

/** Functions over a group's rows, in summarize only. */
export const AGGREGATES: FunctionInfo[] = [
  { name: 'count', args: '', info: 'The number of rows.' },
  { name: 'countif', args: 'condition', info: 'The number of rows the condition is true for.' },
  { name: 'avg', args: 'x', info: 'The mean of a number.' },
  { name: 'sum', args: 'x', info: 'The sum of a number.' },
  { name: 'min', args: 'x', info: 'The least value.' },
  { name: 'max', args: 'x', info: 'The greatest value.' },
  { name: 'percentile', args: 'x, p', info: 'An estimate of the p-th percentile, as percentile(duration, 95).' },
  { name: 'dcount', args: 'x', info: 'An estimate of the number of distinct values.' },
]

/** An operator between two values, with what it means. */
export interface OperatorInfo {
  name: string
  info: string
}

export const OPERATORS: OperatorInfo[] = [
  { name: '==', info: 'Equal. Values of different types are never equal.' },
  { name: '!=', info: 'Not equal; true when the Field is missing.' },
  { name: 'contains', info: 'Contains a substring, ignoring case.' },
  { name: '!contains', info: 'Doesn\'t contain a substring, ignoring case.' },
  { name: 'in', info: 'Equal to a value in the list: in ("a", "b").' },
  { name: '!in', info: 'Equal to none of the values in the list.' },
  { name: 'startswith', info: 'Starts with, ignoring case.' },
  { name: '!startswith', info: 'Doesn\'t start with, ignoring case.' },
  { name: '=~', info: 'Equal, ignoring case.' },
  { name: '!~', info: 'Not equal, ignoring case.' },
  { name: '<', info: 'Less than.' },
  { name: '<=', info: 'Less than or equal.' },
  { name: '>', info: 'Greater than.' },
  { name: '>=', info: 'Greater than or equal.' },
  { name: 'contains_cs', info: 'Contains a substring, with case.' },
  { name: '!contains_cs', info: 'Doesn\'t contain a substring, with case.' },
  { name: 'startswith_cs', info: 'Starts with, with case.' },
  { name: '!startswith_cs', info: 'Doesn\'t start with, with case.' },
  { name: 'matches regex', info: 'Matches an RE2 regular expression, with case.' },
]

/** Words that join or follow conditions. */
export const LOGICAL = [
  { name: 'and', info: 'Both conditions.' },
  { name: 'or', info: 'Either condition.' },
]

/** Operator words: they compare, rather than name Fields. */
export const WORD_OPERATORS = new Set(['and', 'or', 'in', 'contains', 'contains_cs', 'startswith', 'startswith_cs', 'matches', 'regex'])

/** Words with a meaning of their own in some stages. */
export const KEYWORDS = new Set(['by', 'asc', 'desc', 'nulls'])

/** The units a timespan can be written in, as `5m` or `100ms`. */
export const TIMESPAN_UNITS = new Set([
  'd', 'day', 'days', 'h', 'hr', 'hrs', 'hour', 'hours', 'm', 'min', 'minute', 'minutes',
  's', 'sec', 'second', 'seconds', 'ms', 'milli', 'millis', 'millisecond', 'milliseconds',
  'microsecond', 'microseconds', 'tick', 'ticks',
])
