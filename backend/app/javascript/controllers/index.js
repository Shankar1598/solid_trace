// Import and register all your controllers from the importmap via controllers/**/*_controller
import { application } from "controllers/application"
import { eagerLoadControllersFrom } from "@hotwired/stimulus-loading"
eagerLoadControllersFrom("controllers", application)

// Register all ShadCN controllers from the gem
import { registerShadcnControllers } from "shadcn"
registerShadcnControllers(application)
