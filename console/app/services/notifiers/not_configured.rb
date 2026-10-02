# frozen_string_literal: true

module Notifiers
  # Raised by a provider adapter when its Integration lacks the setting it
  # delivers to (webhook URL, routing key or recipients). Nothing was sent.
  class NotConfigured < StandardError; end
end
