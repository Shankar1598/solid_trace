# frozen_string_literal: true

module Notifiers
  # Raised by a provider adapter when the provider did not accept the message:
  # a non-2xx response, a connection error or a timeout.
  class DeliveryFailed < StandardError; end
end
