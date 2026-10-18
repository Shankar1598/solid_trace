import { describe, expect, it } from 'vitest'
import { allTicked, toggleAll, toggleOne } from './selection'

const page = [1, 2, 3]

describe('toggleOne', () => {
  it('ticks an Issue not ticked, and clears one that is', () => {
    expect(toggleOne([1], 2)).toEqual([1, 2])
    expect(toggleOne([1, 2], 1)).toEqual([2])
  })
})

describe('toggleAll', () => {
  it("ticks every Issue on the page unless they're all ticked", () => {
    expect(toggleAll([], page)).toEqual(page)
    expect(toggleAll([2], page)).toEqual(page)
    expect(toggleAll([3, 1, 2], page)).toEqual([])
  })
})

describe('allTicked', () => {
  it('says whether all, some or none of the page are ticked', () => {
    expect(allTicked(page, page)).toBe(true)
    expect(allTicked([2], page)).toBe('some')
    expect(allTicked([], page)).toBe(false)
    expect(allTicked([], [])).toBe(false)
  })
})
