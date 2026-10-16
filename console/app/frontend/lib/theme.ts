// Light or dark: the dark class on <html>, which the layout sets before
// paint from localStorage.theme (or the system's preference). Changing it
// tells every toggle on the page through THEME_CHANGED.

export const THEME_CHANGED = 'solidtrace:theme'

export function isDark(): boolean {
  return document.documentElement.classList.contains('dark')
}

export function setDark(dark: boolean) {
  document.documentElement.classList.toggle('dark', dark)
  try {
    localStorage.theme = dark ? 'dark' : 'light'
  } catch {
    // Without storage the choice lasts until the page reloads.
  }
  window.dispatchEvent(new Event(THEME_CHANGED))
}

export function toggleTheme() {
  setDark(!isDark())
}
