# frozen_string_literal: true

require "test_helper"

# The one token check, for an API token: what McpController and
# introspection act on.
class BearerTokenTest < ActiveSupport::TestCase
  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    @beta = create(:organization, slug: "beta")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:organization_user, organization: @beta, user: @user, role: :member)
    _, @secret = ApiToken.issue!(user: @user, name: "Laptop")
  end

  test "an API token answers its user, every Organization with the role, mcp, no limits, no expiry, the MCP resource and its kind" do
    bearer = BearerToken.authenticate(@secret, issuer: Oauth::Issuer.new("https://console.example.com"))

    assert_equal @user, bearer.user
    assert_equal [ [ "acme", "admin" ], [ "beta", "member" ] ],
                 bearer.memberships.map { |m| [ m.organization.slug, m.role ] }.sort
    assert_equal [ "mcp" ], bearer.scopes
    assert_equal({}, bearer.limits)
    assert_nil bearer.expires_at
    assert_equal "https://console.example.com/mcp", bearer.resource
    assert_equal :api_token, bearer.kind
  end

  test "an API token works with OAuth off, and its resource is the Console URL's MCP server, if any" do
    assert_equal "http://console.example.com/mcp",
                 BearerToken.authenticate(@secret, issuer: Oauth::Issuer.new("http://console.example.com")).resource
    bearer = BearerToken.authenticate(@secret, issuer: Oauth::Issuer.new(nil))
    assert_equal @user, bearer.user
    assert_nil bearer.resource
  end

  test "a membership that is removed is not listed" do
    @user.organization_users.find_by!(organization: @beta).discard!

    assert_equal [ "acme" ], BearerToken.authenticate(@secret).memberships.map { |m| m.organization.slug }
  end

  test "a scoped API token answers its Scopes, its one Organization, its limits and its expiry" do
    expires_at = 1.day.from_now.change(usec: 0)
    _, secret = ApiToken.issue!(user: @user, name: "Run", scopes: [ "sessions:write", "sessions:read" ],
                                organization_id: @beta.id, expires_at: expires_at, agent_session_ids: [ "one", "two" ])

    bearer = BearerToken.authenticate(secret, issuer: Oauth::Issuer.new("https://console.example.com"))

    assert_equal [ "sessions:write", "sessions:read" ], bearer.scopes
    assert_equal [ [ "beta", "member" ] ], bearer.memberships.map { |m| [ m.organization.slug, m.role ] }
    assert_equal @beta, bearer.organization
    assert_equal({ agent_session_ids: [ "one", "two" ] }, bearer.limits)
    assert_equal expires_at, bearer.expires_at
    assert_equal "https://console.example.com/factory", bearer.resource
    assert_not bearer.read_only?
  end

  test "an expired API token, or one whose user left its Organization, answers nil" do
    _, expiring = ApiToken.issue!(user: @user, name: "Run", scopes: [ "factory" ], organization_id: @acme.id,
                                  expires_at: 1.hour.from_now.change(usec: 0))
    _, scoped = ApiToken.issue!(user: @user, name: "Run", scopes: [ "factory" ], organization_id: @beta.id)

    travel_to(ApiToken.find_by_secret(expiring).expires_at) do
      assert_nil BearerToken.authenticate(expiring)
    end
    @user.organization_users.find_by!(organization: @beta).discard!
    assert_nil BearerToken.authenticate(scoped)
  end

  # The discard of a membership deletes its tokens; live? is the check for
  # each other path that removes a membership.
  test "a scoped API token whose user's membership is gone without a discard answers nil" do
    token, scoped = ApiToken.issue!(user: @user, name: "Run", scopes: [ "factory" ], organization_id: @beta.id)
    _, unscoped_secret = ApiToken.issue!(user: @user, name: "Laptop 2")

    OrganizationUser.where(user: @user, organization: @beta).delete_all

    assert ApiToken.exists?(token.id)
    assert_not token.reload.live?
    assert_nil BearerToken.authenticate(scoped)
    assert_not_nil BearerToken.authenticate(unscoped_secret)
  end

  test "mcp:read alone is read only; with mcp it is not" do
    _, read = ApiToken.issue!(user: @user, name: "Read", scopes: [ "mcp:read" ], organization_id: @acme.id)
    _, both = ApiToken.issue!(user: @user, name: "Both", scopes: [ "mcp:read", "mcp" ], organization_id: @acme.id)

    assert BearerToken.authenticate(read).read_only?
    assert_not BearerToken.authenticate(both).read_only?
    assert_not BearerToken.authenticate(@secret).read_only?
  end

  test "an unknown, revoked or malformed secret answers nil" do
    token, revoked = ApiToken.issue!(user: @user, name: "Old")
    token.destroy!

    [ revoked, "st_unknown", "#{@secret} ", @secret.upcase, "sto_#{@secret}", "", nil, 42, [ @secret ] ].each do |secret|
      assert_nil BearerToken.authenticate(secret), secret.inspect
    end
  end
end
