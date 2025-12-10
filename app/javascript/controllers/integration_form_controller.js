import { Controller } from "@hotwired/stimulus"

export default class extends Controller {
  static targets = ["providerSelect", "settingsFields"]

  connect() {
    this.toggleFields()
  }

  toggleFields() {
    const provider = this.providerSelectTarget.value

    // settingsFieldsTarget contains the divs with data-provider
    const fields = this.settingsFieldsTarget.querySelectorAll('[data-provider]')

    fields.forEach(field => {
      const fieldProvider = field.dataset.provider
      if (fieldProvider === provider) {
        field.classList.remove('hidden')
      } else {
        field.classList.add('hidden')
      }
    })
  }
}
