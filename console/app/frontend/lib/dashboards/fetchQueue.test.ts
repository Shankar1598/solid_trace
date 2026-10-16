import { describe, expect, it } from 'vitest'
import { MAX_IN_FLIGHT, createFetchQueue, isAbort } from './fetchQueue'

/** A task that waits until the test settles it, noting its signal. */
function held<T>() {
  const calls: { signal: AbortSignal; resolve: (value: T) => void; reject: (error: unknown) => void }[] = []
  const task = (signal: AbortSignal) => new Promise<T>((resolve, reject) => { calls.push({ signal, resolve, reject }) })
  return { calls, task }
}

const tick = () => new Promise(resolve => setTimeout(resolve, 0))

describe('the chart fetch queue', () => {
  it('runs at most 4 at once and starts the next as one finishes', async () => {
    const queue = createFetchQueue()
    const { calls, task } = held<string>()
    const results = ['a', 'b', 'c', 'd', 'e', 'f'].map(key => queue.run(key, task))

    expect(MAX_IN_FLIGHT).toBe(4)
    expect(calls).toHaveLength(4)
    expect(queue.counts()).toEqual({ inFlight: 4, queued: 2 })

    calls[1].resolve('b done')
    await expect(results[1]).resolves.toBe('b done')
    await tick()
    expect(calls).toHaveLength(5)

    calls[0].reject(new Error('boom'))
    await expect(results[0]).rejects.toThrow('boom')
    await tick()
    expect(calls).toHaveLength(6)
    expect(queue.counts()).toEqual({ inFlight: 4, queued: 0 })
  })

  it('aborts every run, in flight or waiting, and starts none of the waiting', async () => {
    const queue = createFetchQueue(2)
    const { calls, task } = held<string>()
    const results = ['a', 'b', 'c'].map(key => queue.run(key, task))

    queue.abortAll()

    expect(calls.map(c => c.signal.aborted)).toEqual([true, true])
    for (const result of results) await expect(result).rejects.toSatisfy(isAbort)
    await tick()
    expect(calls).toHaveLength(2)
    expect(queue.counts()).toEqual({ inFlight: 0, queued: 0 })
  })

  it('frees an aborted run\'s slot at once, and ignores its late answer', async () => {
    const queue = createFetchQueue(1)
    const { calls, task } = held<string>()
    const first = queue.run('a', task)
    queue.abortAll()
    const second = queue.run('b', task)

    expect(calls).toHaveLength(2)
    calls[0].resolve('late')
    calls[1].resolve('b done')
    await expect(first).rejects.toSatisfy(isAbort)
    await expect(second).resolves.toBe('b done')
  })

  it('lets a newer run for a chart supersede its older one, in flight or waiting', async () => {
    const queue = createFetchQueue(1)
    const { calls, task } = held<string>()
    const older = queue.run('a', task)
    const waiting = queue.run('b', task)
    const newerWaiting = queue.run('b', task)
    const newer = queue.run('a', task)

    expect(calls[0].signal.aborted).toBe(true)
    await expect(older).rejects.toSatisfy(isAbort)
    await expect(waiting).rejects.toSatisfy(isAbort)
    expect(queue.counts()).toEqual({ inFlight: 1, queued: 1 })

    calls[1].resolve('b')
    await expect(newerWaiting).resolves.toBe('b')
    await tick()
    calls[2].resolve('a')
    await expect(newer).resolves.toBe('a')
  })

  it('cancels one chart\'s run and leaves the others', async () => {
    const queue = createFetchQueue()
    const { calls, task } = held<string>()
    const a = queue.run('a', task)
    const b = queue.run('b', task)

    queue.cancel('a')

    await expect(a).rejects.toSatisfy(isAbort)
    calls[1].resolve('b')
    await expect(b).resolves.toBe('b')
  })

  it('passes on a task that throws at once as its error', async () => {
    const queue = createFetchQueue()
    const result = queue.run('a', () => { throw new Error('bad request') })

    await expect(result).rejects.toThrow('bad request')
    expect(queue.counts()).toEqual({ inFlight: 0, queued: 0 })
  })
})
