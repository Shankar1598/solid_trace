# frozen_string_literal: true

InertiaRails.configure do |config|
  # Use ViteRuby digest for cache busting
  config.version = -> { ViteRuby.digest }

  # Default root view
  config.default_render = true
end
