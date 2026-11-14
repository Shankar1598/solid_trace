# frozen_string_literal: true

# The one token check: what a secret sent as Authorization: Bearer stands
# for. McpController and introspection use it, for an API token and an OAuth
# access token, so there is one answer shape for both:
#
# - user: the user the token acts as;
# - memberships: each Organization it acts in, with the user's role there
#   (every Organization of the user for an API token, the Grant's one for
#   an OAuth token);
# - scopes: the names of its Scopes (Oauth::Scopes), mcp for an API token;
# - limits: {} in this effort;
# - expires_at: nil for an API token, which does not expire;
# - issued_at: when the token was made;
# - resource: the resource it is for, <issuer>/mcp for an API token;
# - kind: :api_token or :access_token.
class BearerToken
  Membership = Data.define(:organization, :role)

  attr_reader :user, :scopes, :limits, :expires_at, :issued_at, :resource, :kind

  # The token whose secret this is, or nil for a secret that is unknown,
  # revoked or not a String.
  # An OAuth access token is valid when it is not expired or revoked and
  # its Grant is live (Oauth::Token.find_live_access); its resource is for
  # the caller to compare with its own.
  def self.authenticate(secret, issuer: Oauth::Issuer.current)
    if (access = Oauth::Token.find_live_access(secret, issuer: issuer))
      grant = access.grant
      return new(user: grant.user, scopes: grant.scopes, limits: {}, expires_at: access.expires_at,
                 issued_at: access.created_at, resource: access.resource, kind: :access_token, record: grant)
    end

    api_token = ApiToken.find_by_secret(secret)
    return unless api_token

    new(user: api_token.user, scopes: [ "mcp" ], limits: {}, expires_at: nil, issued_at: api_token.created_at,
        resource: issuer.mcp_resource, kind: :api_token, record: api_token)
  end

  def initialize(user:, scopes:, limits:, expires_at:, issued_at:, resource:, kind:, record:)
    @user = user
    @scopes = scopes
    @limits = limits
    @expires_at = expires_at
    @issued_at = issued_at
    @resource = resource
    @kind = kind
    @record = record
  end

  def memberships
    @memberships ||= begin
      scope = user.organization_users.includes(:organization)
      scope = scope.where(organization_id: @record.organization_id) if oauth?
      scope.map { |membership| Membership.new(organization: membership.organization, role: membership.role) }
    end
  end

  def oauth?
    kind == :access_token
  end

  # The one Organization an OAuth token acts in; nil for an API token, which
  # acts in each of the user's.
  def organization
    @record.organization if oauth?
  end

  # The client_id of an OAuth token's client; nil for an API token.
  def client_id
    @record.client.client_id if oauth?
  end

  # Records a use (at most once a minute): on the API token, or on the
  # OAuth token's Grant.
  def record_use
    @record.touch_last_used
  end
end
