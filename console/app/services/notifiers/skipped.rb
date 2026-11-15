# frozen_string_literal: true

module Notifiers
  # Raised by a provider adapter for a message that it does not send to its
  # Integration, such as a Monitor's resolve to the Factory. Nothing was
  # sent, and the row is skipped with the reason.
  class Skipped < StandardError; end
end
