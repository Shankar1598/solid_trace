# frozen_string_literal: true

# The one token check: what a secret sent as Authorization: Bearer stands
# for. McpController and introspection use it, for an API token and an OAuth
# access token, so there is one answer shape for both:
#
# - user: the user the token acts as;
# - memberships: each Organization it acts in, with the user's role there
#   (the token's one Organization, or every Organization of the user for
#   an API token with none), less each one that has Require two-factor
#   while the user has no Two-factor, as McpTools::Base refuses the token
#   there;
# - scopes: the names of its Scopes (Oauth::Scopes): an API token's, or mcp
#   for one with none; an OAuth token's Grant's Scopes for its resource;
# - limits: an API token's limits (ApiToken#limits); {} for an OAuth token;
# - expires_at: nil for an API token that does not expire;
# - issued_at: when the token was made;
# - resource: the resource it is for: an OAuth token's one; for an API
#   token, the resource of its Scopes, or a list when they have several;
# - kind: :api_token, :access_token, or :console_token for Console's
#   factory token (Oauth::ConsoleToken), which has only the factory Scope
#   in one Organization;
# - parent: for a child token, the Dispatcher's token that made it; else nil.
class BearerToken
  Membership = Data.define(:organization, :role)

  attr_reader :user, :scopes, :limits, :expires_at, :issued_at, :resource, :kind

  # The token whose secret this is, or nil for a secret that is unknown,
  # revoked, expired or not a String, or for an API token whose user left
  # its Organization.
  # An OAuth access token is valid when it is not expired or revoked and
  # its Grant is live (Oauth::Token.find_live_access); its resource is for
  # the caller to compare with its own.
  def self.authenticate(secret, issuer: Oauth::Issuer.current)
    if (access = Oauth::Token.find_live_access(secret, issuer: issuer))
      grant = access.grant
      return new(user: grant.user, scopes: scopes_for(grant.scopes, access.resource, issuer), limits: {},
                 expires_at: access.expires_at, issued_at: access.created_at, resource: access.resource,
                 kind: :access_token, record: grant)
    end

    if (console = Oauth::ConsoleToken.find_live(secret))
      return new(user: console.user, scopes: Oauth::ConsoleToken::SCOPES, limits: {}, expires_at: console.expires_at,
                 issued_at: console.issued_at, resource: resource_of(Oauth::ConsoleToken::SCOPES, issuer),
                 kind: :console_token, record: console)
    end

    api_token = ApiToken.find_by_secret(secret)
    return unless api_token&.live?

    scopes = api_token.effective_scopes
    new(user: api_token.user, scopes: scopes, limits: api_token.limits, expires_at: api_token.expires_at,
        issued_at: api_token.created_at, resource: resource_of(scopes, issuer), kind: :api_token, record: api_token)
  end

  # The names among names whose Scope is for resource: an access token gets
  # only the Grant's Scopes for its own resource (RFC 8707 §2.2).
  def self.scopes_for(names, resource, issuer)
    Oauth::Scopes.for_resource(names, resource, issuer: issuer)
  end

  # The resource of an API token's Scopes: one, a list of several, or nil
  # with no Console URL.
  def self.resource_of(names, issuer)
    resources = names.filter_map { |name| Oauth::Scopes.find(name)&.resource(issuer) }.uniq
    resources.size > 1 ? resources : resources.first
  end
  private_class_method :scopes_for, :resource_of

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
      scope = scope.where(organization_id: @record.organization_id) if @record.organization_id
      scope = scope.joins(:organization).where(organizations: { require_two_factor: false }) unless user.two_factor?
      scope.map { |membership| Membership.new(organization: membership.organization, role: membership.role) }
    end
  end

  def oauth?
    kind == :access_token
  end

  def api_token?
    kind == :api_token
  end

  # The one Organization the token acts in; nil for an API token with none,
  # which acts in each of the user's.
  def organization
    @record.organization
  end

  # Whether the token gives only the MCP server's read tools: it has
  # mcp:read and not mcp, which holds mcp:read.
  def read_only?
    scopes.include?(Oauth::Scopes::MCP_READ) && !scopes.include?(Oauth::Scopes::MCP)
  end

  # The ApiToken; nil for an OAuth token or Console's factory token.
  def api_token
    @record if api_token?
  end

  # The one Project an mcp:read API token reads (its Project limit); nil
  # for a token with no such limit.
  def project
    api_token&.project
  end

  # The token that made this child token; nil for every other token.
  def parent
    api_token&.parent
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
