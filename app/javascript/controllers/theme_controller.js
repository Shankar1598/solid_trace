import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static targets = ["icon"]

  connect() {
    this.updateIcon()
  }

  toggle() {
    if (localStorage.theme === 'dark' || (!('theme' in localStorage) && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
      localStorage.theme = 'light'
      document.documentElement.classList.remove('dark')
    } else {
      localStorage.theme = 'dark'
      document.documentElement.classList.add('dark')
    }
    this.updateIcon()
  }

  updateIcon() {
    // Optional: Update icon based on current theme if we want to show sun/moon
    // For now, we'll just rely on the toggle action
  }
}
