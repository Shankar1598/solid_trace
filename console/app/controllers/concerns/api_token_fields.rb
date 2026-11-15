# frozen_string_literal: true

# Reads the fields of a new API token from a request, as ApiToken takes
# them. A field in the wrong shape raises ArgumentError, so it is refused,
# never dropped: a dropped Scope or limit would make a wider token than the
# one asked for.
module ApiTokenFields
  extend ActiveSupport::Concern

  # One value of a field: a String, a number or nothing.
  SCALAR = [ String, Integer, NilClass ].freeze

  private

  def scalar(fields, key)
    value = fields[key]
    raise ArgumentError, "Give #{key} as one value." unless SCALAR.any? { |kind| value.is_a?(kind) }

    value
  end

  # A list field: nil, or an Array whose items are each one value (an item
  # in another shape becomes a String that ApiToken refuses).
  def list(fields, key)
    value = fields[key]
    return if value.nil?
    raise ArgumentError, "Give #{key} as a list." unless value.is_a?(Array)

    value.map { |item| SCALAR.any? { |kind| item.is_a?(kind) } ? item : item.to_s }
  end
end
