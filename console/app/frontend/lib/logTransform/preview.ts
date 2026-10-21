// A preview of a draft Log transform: the Logs pasted to run it on, and
// EventStore's answer, each Log before and after, dropped or the error.
import type { Log } from '../logs/stream'
import type { ProgramCheck } from './program'

/** One Log's outcome: kept with what it became, dropped by abort, or failed with VRL's error (stored as it arrived). */
export type PreviewResult =
  | { before: Log; after: Log }
  | { before: Log; dropped: true }
  | { before: Log; error: string }

export interface Preview {
  results: PreviewResult[]
  kept: number
  dropped: number
  failed: number
}

/** What a preview answers: the results, or the check's errors when the draft doesn't compile. */
export type PreviewAnswer = { preview: Preview } | { check: ProgramCheck }

/**
 * Logs pasted as a JSON array of objects, or one object, as a sender would
 * send them; or what's wrong with the text. None at all is wrong, as a
 * preview without Logs runs on the recent arrivals.
 */
export function pastedLogs(text: string): { logs: object[] } | { error: string } {
  if (text.trim() === '') return { error: 'Paste at least one Log.' }
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch (error) {
    return { error: `The pasted Logs aren't JSON: ${error instanceof Error ? error.message : String(error)}` }
  }
  if (isObject(value)) return { logs: [value] }
  if (!Array.isArray(value)) return { error: 'Paste a JSON object, or an array of them.' }
  if (value.length === 0) return { error: 'Paste at least one Log.' }
  const notObject = value.findIndex(log => !isObject(log))
  if (notObject >= 0) return { error: `Log ${notObject + 1} isn't a JSON object.` }
  return { logs: value }
}

function isObject(value: unknown): value is object {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
