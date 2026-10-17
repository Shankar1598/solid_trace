# frozen_string_literal: true

# An API token as User settings list it. Never its digest: the secret is
# answered once, when the token is made.
class ApiTokenSerializer
  def initialize(api_token)
    @api_token = api_token
  end

  def as_json(*)
    {
      id: @api_token.id,
      name: @api_token.name,
      created_at: @api_token.created_at.iso8601,
      last_used_at: @api_token.last_used_at&.iso8601,
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
