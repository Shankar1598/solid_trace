# frozen_string_literal: true

module Notifiers
  # Raised by a provider adapter when the receiver answered that it will
  # never take the message, such as the Dispatcher's 4xx other than 429.
  # The row is refused, and no retry sends it again.
  class Refused < DeliveryFailed; end
end
