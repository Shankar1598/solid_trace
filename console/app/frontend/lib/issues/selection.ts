// The Issues ticked on the Issues page, by id.

// Ticks id if it isn't ticked, and clears it if it is.
export function toggleOne(ticked: number[], id: number): number[] {
  return ticked.includes(id) ? ticked.filter(t => t !== id) : [...ticked, id]
}

// Ticks every Issue on the page, or clears them all when they're all ticked.
export function toggleAll(ticked: number[], page: number[]): number[] {
  return allTicked(ticked, page) === true ? [] : page
}

// Whether every Issue on the page is ticked, only some, or none.
export function allTicked(ticked: number[], page: number[]): boolean | 'some' {
  const count = page.filter(id => ticked.includes(id)).length
  if (count === 0) return false
  return count === page.length ? true : 'some'
}
