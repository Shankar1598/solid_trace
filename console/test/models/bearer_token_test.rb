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

  test "an unknown, revoked or malformed secret answers nil" do
    token, revoked = ApiToken.issue!(user: @user, name: "Old")
    token.destroy!

    [ revoked, "st_unknown", "#{@secret} ", @secret.upcase, "sto_#{@secret}", "", nil, 42, [ @secret ] ].each do |secret|
      assert_nil BearerToken.authenticate(secret), secret.inspect
    end
  end
end
