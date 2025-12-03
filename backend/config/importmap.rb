# Pin npm packages by running ./bin/importmap

pin "application"
pin "@hotwired/stimulus", to: "stimulus.min.js"
pin "@hotwired/stimulus-loading", to: "stimulus-loading.js"
pin_all_from "app/javascript/controllers", under: "controllers"
pin "@hotwired/turbo-rails", to: "turbo.min.js"

# shadcn-rails Stimulus controllers
pin "shadcn", to: "shadcn/index.js"
pin "controllers/shadcn/select_controller", to: "controllers/shadcn/select_controller.js"
