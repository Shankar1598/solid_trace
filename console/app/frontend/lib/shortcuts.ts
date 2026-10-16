// Keyboard shortcuts: one registry of every shortcut, which the ? sheet
// lists, and the matching of key presses to them. Pages bind handlers to a
// shortcut; the one bound last runs first, so the top panel's Esc wins.
// While typing only Cmd/Ctrl+K, Cmd/Ctrl+Enter and Esc count, and inside a
// popup (a dialog, menu or list with keys of its own) only Cmd/Ctrl+K.

export type ShortcutId = 'palette' | 'run' | 'focus-query' | 'close' | 'older' | 'newer' | 'help'

export interface Shortcut {
  id: ShortcutId
  /** Its keys, as KeyboardEvent.key names them; Mod is Cmd or Ctrl, either on any platform. */
  keys: string[]
  does: string
  /** Whether it works while typing in an input. */
  whileTyping: boolean
}

export const SHORTCUTS: Shortcut[] = [
  { id: 'palette', keys: ['Mod', 'k'], does: 'Open the command palette', whileTyping: true },
  { id: 'run', keys: ['Mod', 'Enter'], does: 'Run the Query', whileTyping: true },
  { id: 'focus-query', keys: ['/'], does: 'Focus the Query; in the Builder, add a filter', whileTyping: false },
  { id: 'close', keys: ['Escape'], does: 'Close the top panel', whileTyping: true },
  { id: 'older', keys: ['j'], does: 'Next Log in the inspector (older)', whileTyping: false },
  { id: 'newer', keys: ['k'], does: 'Previous Log in the inspector (newer)', whileTyping: false },
  { id: 'help', keys: ['?'], does: 'Show these shortcuts', whileTyping: false },
]

/** What a keydown says of its keys. */
export interface KeyPress {
  key: string
  metaKey: boolean
  ctrlKey: boolean
  altKey: boolean
}

/** Where a press happened: on the page, typing in an input, or in a popup. */
export type Place = 'page' | 'input' | 'popup'

export function shortcutOf(press: KeyPress, place: Place): ShortcutId | null {
  const found = SHORTCUTS.find(s => matches(s.keys, press))
  if (!found) return null
  if (place === 'popup') return found.id === 'palette' ? found.id : null
  return place === 'page' || found.whileTyping ? found.id : null
}

function matches(keys: string[], press: KeyPress): boolean {
  const wantsMod = keys.includes('Mod')
  const key = keys[keys.length - 1]
  if (press.altKey || wantsMod !== (press.metaKey || press.ctrlKey)) return false
  // Cmd+K and Cmd+Shift+K alike; a plain j is not J.
  return wantsMod ? press.key.toLowerCase() === key.toLowerCase() : press.key === key
}

const POPUP = '[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]'
const INPUT = 'input, textarea, select, [contenteditable]:not([contenteditable="false"])'

/** Where a press on `target` happened. */
export function placeOf(target: { closest: (selector: string) => unknown } | null): Place {
  if (!target || typeof target.closest !== 'function') return 'page'
  if (target.closest(POPUP)) return 'popup'
  return target.closest(INPUT) ? 'input' : 'page'
}

/** A shortcut's keys as a Mac (⌘K, ⌘↵) or anything else (Ctrl+K) shows them. */
export function keyLabel(keys: string[], mac: boolean): string {
  const names = keys.map(key => {
    if (key === 'Mod') return mac ? '⌘' : 'Ctrl'
    if (key === 'Enter') return mac ? '↵' : 'Enter'
    if (key === 'Escape') return 'Esc'
    return key.length === 1 && keys.length > 1 ? key.toUpperCase() : key
  })
  return names.join(mac ? '' : '+')
}

/** A handler answers false to pass the press on to the one bound before it. */
type Handler = () => boolean
const bound: { id: ShortcutId; handler: Handler }[] = []

/** Binds a handler to a shortcut; the returned function unbinds it. */
export function bindShortcut(id: ShortcutId, handler: Handler): () => void {
  const entry = { id, handler }
  bound.push(entry)
  return () => {
    const at = bound.indexOf(entry)
    if (at >= 0) bound.splice(at, 1)
  }
}

/** Runs the press's shortcut by the handler bound last that takes it; whether one did. */
export function dispatchShortcut(press: KeyPress, place: Place): boolean {
  const id = shortcutOf(press, place)
  if (!id) return false
  for (let i = bound.length - 1; i >= 0; i--) {
    if (bound[i].id === id && bound[i].handler()) return true
  }
  return false
}
