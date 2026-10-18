import { describe, expect, it } from 'vitest'
import { SHORTCUTS, bindShortcut, dispatchShortcut, keyLabel, placeOf, shortcutOf, type KeyPress } from './shortcuts'

const press = (key: string, mods: Partial<KeyPress> = {}): KeyPress => ({ key, metaKey: false, ctrlKey: false, altKey: false, ...mods })

describe('matching a key press to a shortcut', () => {
  it('takes Cmd or Ctrl as Mod, on any platform', () => {
    expect(shortcutOf(press('k', { metaKey: true }), 'page')).toBe('palette')
    expect(shortcutOf(press('K', { ctrlKey: true }), 'page')).toBe('palette')
    expect(shortcutOf(press('Enter', { ctrlKey: true }), 'page')).toBe('run')
    expect(shortcutOf(press('Enter'), 'page')).toBeNull()
  })

  it('reads single keys as typed, with no Cmd, Ctrl or Alt', () => {
    expect(shortcutOf(press('/'), 'page')).toBe('focus-query')
    expect(shortcutOf(press('?'), 'page')).toBe('help')
    expect(shortcutOf(press('j'), 'page')).toBe('older')
    expect(shortcutOf(press('k'), 'page')).toBe('newer')
    expect(shortcutOf(press('Escape'), 'page')).toBe('close')
    expect(shortcutOf(press('J'), 'page')).toBeNull()
    expect(shortcutOf(press('j', { altKey: true }), 'page')).toBeNull()
    expect(shortcutOf(press('k', { ctrlKey: true, altKey: true }), 'page')).toBeNull()
  })

  it('while typing, takes only Cmd/Ctrl+K, Cmd/Ctrl+Enter and Esc', () => {
    const typed = ['/', '?', 'j', 'k'].map(key => shortcutOf(press(key), 'input'))
    expect(typed).toEqual([null, null, null, null])
    expect(shortcutOf(press('k', { metaKey: true }), 'input')).toBe('palette')
    expect(shortcutOf(press('Enter', { metaKey: true }), 'input')).toBe('run')
    expect(shortcutOf(press('Escape'), 'input')).toBe('close')
  })

  it('in a popup, which has its own keys, takes only the palette\'s', () => {
    expect(shortcutOf(press('Escape'), 'popup')).toBeNull()
    expect(shortcutOf(press('j'), 'popup')).toBeNull()
    expect(shortcutOf(press('k', { ctrlKey: true }), 'popup')).toBe('palette')
  })

  it('places a press by the element it came from', () => {
    const at = (match: string | null) => ({ closest: (selector: string) => (match && selector.includes(match) ? {} : null) })
    expect(placeOf(at('[role="dialog"]'))).toBe('popup')
    expect(placeOf(at('textarea'))).toBe('input')
    expect(placeOf(at('[contenteditable]'))).toBe('input')
    expect(placeOf(at(null))).toBe('page')
    expect(placeOf(null)).toBe('page')
  })
})

describe('the shortcuts registry', () => {
  it('lists each shortcut once, with its keys for a Mac and elsewhere', () => {
    expect(new Set(SHORTCUTS.map(s => s.id)).size).toBe(SHORTCUTS.length)
    const palette = SHORTCUTS.find(s => s.id === 'palette')!
    expect(keyLabel(palette.keys, true)).toBe('⌘K')
    expect(keyLabel(palette.keys, false)).toBe('Ctrl+K')
    expect(keyLabel(['Mod', 'Enter'], true)).toBe('⌘↵')
    expect(keyLabel(['Escape'], false)).toBe('Esc')
  })

  it('runs the handler bound last, passing on when it declines, until unbound', () => {
    const ran: string[] = []
    const unbindPage = bindShortcut('close', () => { ran.push('page'); return true })
    const unbindPanel = bindShortcut('close', () => { ran.push('panel'); return true })
    const unbindIdle = bindShortcut('close', () => false)

    expect(dispatchShortcut(press('Escape'), 'page')).toBe(true)
    unbindPanel()
    expect(dispatchShortcut(press('Escape'), 'page')).toBe(true)
    unbindPage()
    unbindIdle()
    expect(dispatchShortcut(press('Escape'), 'page')).toBe(false)
    expect(ran).toEqual(['panel', 'page'])
    expect(dispatchShortcut(press('x'), 'page')).toBe(false)
  })
})
