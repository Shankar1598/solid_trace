import { Controller } from "@hotwired/stimulus"
import { Turbo } from "@hotwired/turbo-rails"

export default class extends Controller {
  static targets = ["trigger", "content", "input", "display"]
  static values = {
    value: String,
    open: { type: Boolean, default: false }
  }

  connect() {
    this.clickOutside = this.clickOutside.bind(this)
    if (this.hasValueValue) {
      this.updateDisplay(this.valueValue)
    }
  }

  toggle(event) {
    event.preventDefault()
    this.openValue = !this.openValue
  }

  close(event) {
    if (event) event.preventDefault()
    this.openValue = false
  }

  openValueChanged() {
    if (this.openValue) {
      this.contentTarget.hidden = false
      this.contentTarget.dataset.state = "open"
      this.triggerTarget.setAttribute("aria-expanded", "true")
      document.addEventListener("click", this.clickOutside)
    } else {
      this.contentTarget.hidden = true
      this.contentTarget.dataset.state = "closed"
      this.triggerTarget.setAttribute("aria-expanded", "false")
      document.removeEventListener("click", this.clickOutside)
    }
  }

  select(event) {
    const item = event.currentTarget
    const value = item.dataset.value
    const text = item.textContent.trim()

    this.valueValue = value
    this.inputTarget.value = value
    this.displayTarget.textContent = text

    // Update visual state of items
    this.element.querySelectorAll('[role="option"]').forEach(option => {
      if (option.dataset.value === value) {
        option.setAttribute("aria-selected", "true")
        option.dataset.state = "checked"
        // Show check icon if present
        const check = option.querySelector("span")
        if (check) check.classList.remove("opacity-0")
      } else {
        option.setAttribute("aria-selected", "false")
        option.dataset.state = "unchecked"
        // Hide check icon if present
        const check = option.querySelector("span")
        if (check) check.classList.add("opacity-0")
      }
    })

    this.close()
    this.dispatch("change", { detail: { value } })

    // Trigger form submission if needed (e.g. for filters)
    if (this.element.closest("form")) {
      this.element.closest("form").requestSubmit()
    } else {
      // If not in a form, we might want to trigger a visit or reload
      // For now, let's assume it's used in a form or with another controller listening to change
      const url = new URL(window.location)
      url.searchParams.set(this.inputTarget.name, value)
      if (value === "all") url.searchParams.delete(this.inputTarget.name)
      Turbo.visit(url)
    }
  }

  clickOutside(event) {
    if (!this.element.contains(event.target)) {
      this.close()
    }
  }

  handleKeydown(event) {
    if (!this.openValue) {
      if (event.key === "Enter" || event.key === " " || event.key === "ArrowDown") {
        this.toggle(event)
      }
      return
    }

    if (event.key === "Escape") {
      this.close(event)
      return
    }
  }

  updateDisplay(value) {
    const item = this.element.querySelector(`[role="option"][data-value="${value}"]`)
    if (item) {
      this.displayTarget.textContent = item.textContent.trim()
    }
  }
}
