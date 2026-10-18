// Runs a Dashboard's charts a few at a time, so a page of many charts
// doesn't send EventStore every Query at once. Each run is keyed by its
// chart: a newer run for a chart supersedes the older one, and a range change
// or leaving the page aborts them all. An aborted run's promise rejects with
// an AbortError (see isAbort) and its slot is free at once. A cancelled run's
// slot goes to a waiting run only once the task that cancelled it ends: on a
// range change each card cancels its own run, and none of the old range's
// waiting runs should start in between. The browser's slot is not the
// server's: Console keeps an aborted run's thread until EventStore answers,
// and caps those runs, with the explorer's, itself (app/services/chart_runs.rb).

/** The most chart runs in flight at once. Each holds a Console Puma thread, so keep it under ChartRuns' cap (test/config/puma_test.rb). */
export const MAX_IN_FLIGHT = 4

export type Task<T> = (signal: AbortSignal) => Promise<T>

export interface FetchQueue {
  /** Runs `task` for `key` once a slot is free, aborting any run of `key` before it. */
  run<T>(key: string, task: Task<T>): Promise<T>
  /** Aborts `key`'s run, in flight or waiting. */
  cancel(key: string): void
  /** Aborts every run, in flight or waiting. */
  abortAll(): void
  counts(): { inFlight: number; queued: number }
}

interface Entry {
  key: string
  task: Task<unknown>
  controller: AbortController
  resolve: (value: unknown) => void
  reject: (error: unknown) => void
}

/** Whether an error is a run's abort, which a chart shows as nothing. */
export function isAbort(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError'
}

function abortError(): Error {
  const error = new Error('The run was aborted.')
  error.name = 'AbortError'
  return error
}

export function createFetchQueue(limit = MAX_IN_FLIGHT): FetchQueue {
  const waiting: Entry[] = []
  const inFlight = new Map<string, Entry>()

  const pump = () => {
    while (inFlight.size < limit && waiting.length > 0) start(waiting.shift()!)
  }

  const start = (entry: Entry) => {
    inFlight.set(entry.key, entry)
    let answer: Promise<unknown>
    try {
      answer = Promise.resolve(entry.task(entry.controller.signal))
    } catch (error) {
      answer = Promise.reject(error)
    }
    answer.then(
      value => settle(entry, () => entry.resolve(value)),
      error => settle(entry, () => entry.reject(error)),
    )
  }

  // An aborted entry was rejected when it was aborted, and its slot freed.
  const settle = (entry: Entry, done: () => void) => {
    if (entry.controller.signal.aborted) return
    inFlight.delete(entry.key)
    done()
    pump()
  }

  const abort = (entry: Entry) => {
    entry.controller.abort()
    entry.reject(abortError())
    if (inFlight.get(entry.key) === entry) inFlight.delete(entry.key)
    else waiting.splice(waiting.indexOf(entry), 1)
  }

  const find = (key: string) => inFlight.get(key) ?? waiting.find(e => e.key === key)

  return {
    run<T>(key: string, task: Task<T>): Promise<T> {
      const older = find(key)
      if (older) abort(older)
      const answer = new Promise<T>((resolve, reject) => {
        waiting.push({ key, task, controller: new AbortController(), resolve: resolve as (value: unknown) => void, reject })
      })
      pump()
      return answer
    },
    cancel(key: string) {
      const entry = find(key)
      if (!entry) return
      abort(entry)
      queueMicrotask(pump)
    },
    abortAll() {
      for (const entry of [...inFlight.values(), ...waiting]) abort(entry)
    },
    counts: () => ({ inFlight: inFlight.size, queued: waiting.length }),
  }
}
