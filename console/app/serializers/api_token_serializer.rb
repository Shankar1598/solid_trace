# frozen_string_literal: true

# An API token as User settings list it: its Scopes, Organization, limits
# and expiry. Never its digest: the secret is answered once, when the token
# is made.
class ApiTokenSerializer
  def initialize(api_token)
    @api_token = api_token
  end

  def as_json(*)
    organization = @api_token.organization
    {
      id: @api_token.id,
      name: @api_token.name,
      created_at: @api_token.created_at.iso8601,
      last_used_at: @api_token.last_used_at&.iso8601,
      scopes: @api_token.scopes,
      organization: organization && { id: organization.id, slug: organization.slug, name: organization.name },
      limits: @api_token.limits,
      expires_at: @api_token.expires_at&.iso8601,
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
