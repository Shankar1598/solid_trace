// The captured repositories list of Organization settings → Agent sessions,
// typed one entry on each line. Console checks each entry at save
// (Project#captured_repositories_well_formed); here the page only finds the
// entries a save adds, as those need the confirm.

/** An entry as Console stores it: no spaces, the host in lower case, no `.git` ending. */
export function normaliseEntry(entry: string): string {
  const trimmed = entry.trim()
  const slash = trimmed.indexOf('/')
  const normalised = slash < 0 ? trimmed.toLowerCase() : trimmed.slice(0, slash).toLowerCase() + trimmed.slice(slash)
  return normalised.endsWith('.git') ? normalised.slice(0, -'.git'.length) : normalised
}

/** The entries of the typed text, one on each line, each once, with no empty lines. */
export function parseEntries(text: string): string[] {
  return [...new Set(text.split('\n').map(normaliseEntry).filter(entry => entry !== ''))]
}

/** The entries of draft that saved doesn't have: new capture, which needs the confirm. */
export function addedEntries(saved: string[], draft: string[]): string[] {
  return draft.filter(entry => !saved.includes(entry))
}
