import { Application } from "@hotwired/stimulus"

const application = Application.start()

// Configure Stimulus development experience
application.debug = false
window.Stimulus   = application

export { application }

// Import and register shadcn-rails controllers
import { registerShadcnControllers } from "shadcn"
registerShadcnControllers(application)
