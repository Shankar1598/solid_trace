# frozen_string_literal: true

require "test_helper"

# Seam 2: a member makes an API token with Scopes, limits, one Organization
# and an expiry on Settings → API tokens & MCP, and the page shows them. A
# token that asks for more than the user can give is refused at save.
class ApiTokenScopesTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @user = create(:user)
    @acme = create(:organization, name: "Acme", slug: "acme")
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    @outsider_org = create(:organization, name: "Other", slug: "other")
    @outsider_project = create(:project, organization: @outsider_org, slug: "theirs")
    sign_in_as(@user)
  end

  test "a member makes a token with factory scopes, one Organization and an expiry, and the page shows them" do
    expires_at = 30.days.from_now.change(usec: 0)

    make(scopes: [ "sessions:write", "factory" ], organization_id: @acme.id, expires_at: expires_at.iso8601)

    assert_response :created
    token = @user.api_tokens.sole
    answered = response.parsed_body["api_token"]
    assert_equal [ "sessions:write", "factory" ], answered["scopes"]
    assert_equal({ "id" => @acme.id, "slug" => "acme", "name" => "Acme" }, answered["organization"])
    assert_equal expires_at.iso8601, answered["expires_at"]
    assert_equal({}, answered["limits"])

    listed = page_props["api_tokens"].sole
    assert_equal token.id, listed["id"]
    assert_equal [ "sessions:write", "factory" ], listed["scopes"]
    assert_equal "acme", listed.dig("organization", "slug")
    assert_equal expires_at.iso8601, listed["expires_at"]
  end

  test "a token with no scope, no Organization and no expiry keeps today's meaning" do
    make(name: "Laptop")

    assert_response :created
    answered = response.parsed_body["api_token"]
    assert_equal [], answered["scopes"]
    assert_nil answered["organization"]
    assert_nil answered["expires_at"]
    assert_equal({}, answered["limits"])
  end

  test "the limits are saved and shown: Agent session ids for sessions:*, a Project for mcp:read" do
    make(scopes: [ "sessions:read", "sessions:write" ], organization_id: @acme.id,
         agent_session_ids: [ "0f8a5c1e-1b2c-4d3e-8f90-123456789abc", "second-id" ])
    assert_response :created
    assert_equal({ "agent_session_ids" => [ "0f8a5c1e-1b2c-4d3e-8f90-123456789abc", "second-id" ] },
                 response.parsed_body.dig("api_token", "limits"))

    make(scopes: [ "mcp:read" ], organization_id: @acme.id, project_id: @shop.id)
    assert_response :created
    assert_equal({ "project" => { "id" => @shop.id, "slug" => "shop", "name" => "Shop" } },
                 response.parsed_body.dig("api_token", "limits"))

    assert_equal 2, page_props["api_tokens"].count { |token| token["limits"].present? }
  end

  test "the page offers each scope that a person can give, with its text, and not tokens:create" do
    offered = page_props["scopes"]

    assert_equal [ "mcp", "mcp:read", "sessions:write", "sessions:read", "run_events:write", "factory", "issues:comment" ],
                 offered.pluck("name")
    offered.each { |scope| assert scope["text"].present?, scope["name"] }
    assert_not_includes offered.pluck("name"), "tokens:create"
    assert_equal [ { "id" => @acme.id, "slug" => "acme", "name" => "Acme" } ], page_props["organizations"]
  end

  # Refusals at save

  test "a scope that is not in the registry is refused with a clear error" do
    [ [ "admin" ], [ "mcp", "MCP" ], [ "factory " ], [ "" ] ].each do |scopes|
      refused(scopes: scopes, organization_id: @acme.id)
      assert_match(/is not a scope/, errors["scopes"].join, scopes.inspect)
    end
  end

  # A field that was dropped would make a wider token than the one asked
  # for: an unscoped token, or mcp:read with no Project limit.
  test "a field in the wrong shape is refused, never dropped" do
    [ { scopes: "factory" }, { scopes: { name: "factory" } }, { scopes: [ { name: "factory" } ] },
      { scopes: [ "mcp:read" ], project_id: { id: @shop.id } }, { scopes: [ "mcp:read" ], project_id: [ @shop.id ] },
      { scopes: [ "sessions:write" ], agent_session_ids: "one" }, { scopes: [ "sessions:write" ], agent_session_ids: [ 1 ] },
      { scopes: [ "factory" ], expires_at: 1_900_000_000 }, { scopes: [ "factory" ], organization_id: [ @acme.id ] } ].each do |fields|
      refused(organization_id: @acme.id, **fields)
      assert errors.values.flatten.any?, fields.inspect
    end
  end

  test "tokens:create is refused for a person's own token" do
    refused(scopes: [ "tokens:create" ], organization_id: @acme.id)

    assert_match "tokens:create", errors["scopes"].join
  end

  test "an Organization that the user is not a member of, or left, is refused" do
    refused(scopes: [ "factory" ], organization_id: @outsider_org.id)
    assert_match "not one of your Organizations", errors["organization"].join

    refused(scopes: [ "factory" ], organization_id: 0)
    assert_match "not one of your Organizations", errors["organization"].join

    create(:organization_user, organization: @beta, user: @user, role: :member).discard!
    refused(scopes: [ "factory" ], organization_id: @beta.id)
    assert_match "not one of your Organizations", errors["organization"].join
  end

  test "a token with scopes needs an Organization" do
    refused(scopes: [ "factory" ])

    assert_match "needs one Organization", errors["organization"].join
  end

  test "an expiry in the past, now, or not a time is refused" do
    [ 1.minute.ago.iso8601, Time.current.iso8601, "next week", "2026-13-40T00:00:00Z" ].each do |expires_at|
      refused(scopes: [ "factory" ], organization_id: @acme.id, expires_at: expires_at)
      assert errors["expires_at"].present?, expires_at
    end
  end

  test "sessions:read is made only with a list of Agent session ids" do
    refused(scopes: [ "sessions:read" ], organization_id: @acme.id)
    assert_match "needs a list of Agent session ids", errors["limits"].join

    refused(scopes: [ "sessions:read" ], organization_id: @acme.id, agent_session_ids: [])
    assert_match "Give the Agent session ids as a list", errors["limits"].join
  end

  # An empty list reaches no Agent session. If it became "no list", the
  # token would reach every Agent session of the Organization.
  test "an empty list of Agent session ids is refused, never made into no limit" do
    [ [], [ nil ] ].each do |ids|
      refused(scopes: [ "sessions:write" ], organization_id: @acme.id, agent_session_ids: ids)
      assert_match "Give the Agent session ids as a list", errors["limits"].join, ids.inspect
    end
  end

  test "Agent session ids must be well formed, and at most 100" do
    [ [ "" ], [ "has space" ], [ "a" * 129 ], [ "../etc" ], Array.new(101) { |i| "id-#{i}" } ].each do |ids|
      refused(scopes: [ "sessions:write" ], organization_id: @acme.id, agent_session_ids: ids)
      assert errors["limits"].present?, ids.first
    end
  end

  test "a list of Agent session ids needs sessions:write or sessions:read" do
    refused(scopes: [ "mcp:read" ], organization_id: @acme.id, agent_session_ids: [ "one" ])

    assert_match "sessions:write or sessions:read", errors["limits"].join
  end

  test "a Project limit needs mcp:read, without mcp, and a Project of the token's Organization" do
    refused(scopes: [ "factory" ], organization_id: @acme.id, project_id: @shop.id)
    assert_match "mcp:read", errors["limits"].join

    refused(scopes: [ "mcp:read", "mcp" ], organization_id: @acme.id, project_id: @shop.id)
    assert_match "mcp reaches every Project", errors["limits"].join

    refused(scopes: [ "mcp:read" ], organization_id: @acme.id, project_id: @outsider_project.id)
    assert_match "not a Project of Acme", errors["limits"].join

    refused(scopes: [ "mcp:read" ], organization_id: @acme.id, project_id: 0)
    assert_match "not a Project of Acme", errors["limits"].join
  end

  private

  def make(name: "Factory", **fields)
    post api_tokens_url, params: { api_token: { name: name, **fields } }, as: :json
  end

  def refused(**fields)
    assert_no_difference -> { ApiToken.count } do
      make(**fields)
    end
    assert_response :unprocessable_content
  end

  def errors
    response.parsed_body["errors"]
  end

  def page_props
    get api_token_settings_url(org_slug: @acme.slug), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    response.parsed_body["props"]
  end
end
