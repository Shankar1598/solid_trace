# frozen_string_literal: true

require "test_helper"

# Seam 2: the Dispatcher's token and its child tokens (GLOSSARY: API token).
# An Admin makes the Dispatcher's token on Settings → API tokens & MCP. The
# Dispatcher sends it to POST /api/child_tokens to make a child token for
# one Execution, and to DELETE /api/child_tokens/:id to revoke one. A child
# is never wider than its parent and never outlives it.
class ChildTokensTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  DISPATCHER_SCOPES = [ "run_events:write", "issues:comment", "tokens:create", "mcp:read" ].freeze
  SESSION_A = "0f8a5c1e-1b2c-4d3e-8f90-123456789abc"
  SESSION_B = "7d1e5a90-2c4b-4f7a-8e61-5b3c9d0a2e17"

  setup do
    @admin = create(:user, name: "Dana", email: "dana@example.com")
    @member = create(:user, name: "Mo", email: "mo@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @admin, role: :admin)
    create(:organization_user, organization: @acme, user: @member, role: :member)
    create(:organization_user, organization: @beta, user: @member, role: :admin)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    @agent_project = create(:project, organization: @acme, name: "Agent sessions", slug: "agent-sessions", agent: true)
    @theirs = create(:project, organization: @beta, slug: "theirs")
    _, @caller = ApiToken.issue!(user: create(:user), name: "Service")
    @parent_expiry = 30.days.from_now.change(usec: 0)
  end

  # The Dispatcher's token

  test "an Admin makes the Dispatcher's token, and the page shows it" do
    sign_in_as(@admin)
    make_on_page(scopes: DISPATCHER_SCOPES, organization_id: @acme.id, expires_at: @parent_expiry.iso8601)

    assert_response :created
    assert_equal DISPATCHER_SCOPES, response.parsed_body.dig("api_token", "scopes")
    assert_equal true, response.parsed_body.dig("api_token", "dispatcher")

    props = page_props
    assert_equal DISPATCHER_SCOPES, props.dig("dispatcher", "scopes")
    assert_equal [ "acme" ], props.dig("dispatcher", "organizations").pluck("slug")
    assert_equal [ true ], props["api_tokens"].pluck("dispatcher")
  end

  test "a member cannot make the Dispatcher's token, also when they are an Admin of another Organization" do
    sign_in_as(@member)

    refused_on_page(scopes: DISPATCHER_SCOPES, organization_id: @acme.id)
    assert_match(/Only an Admin of Acme/, errors["scopes"].join)
    assert_equal [ "beta" ], page_props.dig("dispatcher", "organizations").pluck("slug")
  end

  test "tokens:create goes only with the Dispatcher's scopes, and with no limit" do
    sign_in_as(@admin)

    [ { scopes: [ "tokens:create" ] }, { scopes: DISPATCHER_SCOPES + [ "factory" ] },
      { scopes: DISPATCHER_SCOPES + [ "sessions:write" ], agent_session_ids: [ SESSION_A ] },
      { scopes: DISPATCHER_SCOPES, project_id: @shop.id } ].each do |fields|
      refused_on_page(organization_id: @acme.id, **fields)
      assert_match "tokens:create", errors.values.flatten.join, fields.inspect
    end

    [ { project_id: @shop.id }, { agent_session_ids: [ SESSION_A ] } ].each do |limit|
      refused_on_page(scopes: DISPATCHER_SCOPES, organization_id: @acme.id, **limit)
      assert_match(/has no limit/, errors["limits"].join, limit.inspect)
    end
  end

  test "the Dispatcher's token acts as its Admin, in its one Organization, within its scopes" do
    parent, secret = dispatcher_token

    introspect(secret)

    body = response.parsed_body
    assert_equal true, body["active"]
    assert_equal @admin.id.to_s, body["sub"]
    assert_equal DISPATCHER_SCOPES.join(" "), body["scope"]
    assert_equal [ { "id" => @acme.id, "slug" => "acme", "name" => "Acme", "role" => "admin" } ], body["organizations"]
    assert_equal @parent_expiry.to_i, body["exp"]
    assert_nil body["parent"]
    assert parent.dispatcher?
  end

  # Making a child token

  test "the Dispatcher makes a child token for one Execution, and introspection names its parent" do
    parent, secret = dispatcher_token
    expires_at = 3.hours.from_now.change(usec: 0)

    make_child(secret, name: "Run 42, Execution 1", scopes: [ "sessions:write", "sessions:read" ],
                       agent_session_ids: [ SESSION_A, SESSION_B ], expires_at: expires_at.iso8601)

    assert_response :created
    made = response.parsed_body
    assert_equal [ "sessions:write", "sessions:read" ], made.dig("api_token", "scopes")
    assert_equal({ "agent_session_ids" => [ SESSION_A, SESSION_B ] }, made.dig("api_token", "limits"))
    assert_equal expires_at.iso8601, made.dig("api_token", "expires_at")

    introspect(made["secret"])
    body = response.parsed_body
    assert_equal true, body["active"]
    assert_equal @admin.id.to_s, body["sub"]
    assert_equal "sessions:write sessions:read", body["scope"]
    assert_equal({ "agent_session_ids" => [ SESSION_A, SESSION_B ] }, body["limits"])
    assert_equal expires_at.to_i, body["exp"]
    assert_equal({ "id" => parent.id, "name" => "Dispatcher", "dispatcher" => true }, body["parent"])
    assert_equal [ "acme" ], body["organizations"].pluck("slug")
  end

  test "a child token with mcp:read for one Project of the Organization is made" do
    _, secret = dispatcher_token

    make_child(secret, scopes: [ "mcp:read" ], project_id: @shop.id, expires_at: 1.hour.from_now.iso8601)

    assert_response :created
    assert_equal({ "project" => { "id" => @shop.id, "slug" => "shop", "name" => "Shop" } },
                 response.parsed_body.dig("api_token", "limits"))
  end

  test "a child token may have the parent's own scopes, but never tokens:create" do
    _, secret = dispatcher_token

    make_child(secret, scopes: [ "run_events:write", "issues:comment" ], expires_at: 1.hour.from_now.iso8601)
    assert_response :created

    refused_child(secret, scopes: [ "tokens:create" ], expires_at: 1.hour.from_now.iso8601)
    assert_match "tokens:create", errors["scopes"].join
  end

  test "a child token with a scope that its parent cannot give is refused" do
    _, secret = dispatcher_token

    [ [ "factory" ], [ "mcp" ], [ "sessions:write", "factory" ], [ "sessions:read", "sessions:download" ],
      [ "admin" ] ].each do |scopes|
      refused_child(secret, scopes: scopes, agent_session_ids: [ SESSION_A ], expires_at: 1.hour.from_now.iso8601)
      assert errors["scopes"].present?, scopes.inspect
    end
  end

  # A child with no scope would act as the Admin over MCP, in each
  # Organization: the widest token there is.
  test "a child token with no scope is refused" do
    _, secret = dispatcher_token

    [ [], nil ].each do |scopes|
      refused_child(secret, scopes: scopes, expires_at: 1.hour.from_now.iso8601)
      assert_match(/needs at least one scope/, errors["scopes"].join, scopes.inspect)
    end
  end

  test "a child token with a wider limit is refused: sessions:* for any Agent session, mcp:read for any Project" do
    _, secret = dispatcher_token
    expires_at = 1.hour.from_now.iso8601

    refused_child(secret, scopes: [ "sessions:write" ], expires_at: expires_at)
    assert_match(/Agent session ids/, errors["limits"].join)

    refused_child(secret, scopes: [ "mcp:read" ], expires_at: expires_at)
    assert_match(/one Project/, errors["limits"].join)

    refused_child(secret, scopes: [ "mcp:read" ], project_id: @agent_project.id, expires_at: expires_at)
    assert_match(/Agent Project/, errors["limits"].join)

    refused_child(secret, scopes: [ "mcp:read" ], project_id: @theirs.id, expires_at: expires_at)
    assert_match(/not a Project of Acme/, errors["limits"].join)
  end

  test "a child token's expiry is no later than its parent's, and it must have one" do
    _, secret = dispatcher_token
    fields = { scopes: [ "run_events:write" ] }

    refused_child(secret, **fields, expires_at: (@parent_expiry + 1.second).iso8601)
    assert_match(/no later than/, errors["expires_at"].join)

    refused_child(secret, **fields)
    assert_match(/needs an expiry/, errors["expires_at"].join)

    refused_child(secret, **fields, expires_at: 1.second.ago.iso8601)
    assert_match(/in the future/, errors["expires_at"].join)

    make_child(secret, **fields, expires_at: @parent_expiry.iso8601)
    assert_response :created
  end

  test "a parent with no expiry gives children any future expiry" do
    _, secret = dispatcher_token(expires_at: nil)

    make_child(secret, scopes: [ "run_events:write" ], expires_at: 2.years.from_now.iso8601)

    assert_response :created
  end

  test "a field in the wrong shape is refused, never dropped" do
    _, secret = dispatcher_token
    expires_at = 1.hour.from_now.iso8601

    [ { scopes: "sessions:write", agent_session_ids: [ SESSION_A ] },
      { scopes: [ "sessions:write" ], agent_session_ids: SESSION_A },
      { scopes: [ "mcp:read" ], project_id: [ @shop.id ] } ].each do |fields|
      refused_child(secret, expires_at: expires_at, **fields)
    end
    refused_child(secret, scopes: [ "run_events:write" ], expires_at: "tomorrow")
    assert_match(/date and time/, errors["expires_at"].join)
  end

  test "only a live token with tokens:create makes a child token" do
    _, person = ApiToken.issue!(user: @admin, name: "Laptop", scopes: [ "run_events:write" ], organization_id: @acme.id)
    _, unscoped = ApiToken.issue!(user: @admin, name: "MCP")
    _, dispatcher = dispatcher_token
    make_child(dispatcher, scopes: [ "run_events:write" ], expires_at: 1.hour.from_now.iso8601)
    child = response.parsed_body["secret"]

    [ person, unscoped, child ].each do |secret|
      refused_child(secret, scopes: [ "run_events:write" ], expires_at: 1.hour.from_now.iso8601, status: :forbidden)
      assert_equal "insufficient_scope", response.parsed_body["error"]
      assert_equal "tokens:create", response.parsed_body["scope"]
    end

    [ nil, "st_unknown", "not a token" ].each do |secret|
      refused_child(secret, scopes: [ "run_events:write" ], expires_at: 1.hour.from_now.iso8601, status: :unauthorized)
      assert_match(/Bearer/, response.headers["WWW-Authenticate"])
    end
  end

  # The model never gives a child tokens:create. If a child had it, it still
  # must not make children, or it could make one that outlives it.
  test "a child token with tokens:create still makes no child token" do
    _, dispatcher = dispatcher_token
    child_id, child = child_token(dispatcher)
    ApiToken.find(child_id).update_column(:scopes, DISPATCHER_SCOPES)

    refused_child(child, scopes: [ "run_events:write" ], expires_at: 1.hour.from_now.iso8601, status: :forbidden)
    assert_equal "insufficient_scope", response.parsed_body["error"]
  end

  test "the Dispatcher's token makes no child token while its Organization requires Two-factor that its Admin lacks" do
    _, secret = dispatcher_token
    @acme.update!(require_two_factor: true)

    refused_child(secret, scopes: [ "run_events:write" ], expires_at: 1.hour.from_now.iso8601, status: :forbidden)
    assert_match(/Two-factor/, response.parsed_body["error_description"])
  end

  test "an expired Dispatcher's token makes no child token" do
    _, secret = dispatcher_token(expires_at: 1.hour.from_now)

    travel_to(2.hours.from_now) do
      refused_child(secret, scopes: [ "run_events:write" ], expires_at: 3.hours.from_now.iso8601, status: :unauthorized)
    end
  end

  # Revoking

  test "the Dispatcher revokes a child token, and it stops at once" do
    _, secret = dispatcher_token
    child_id, child = child_token(secret)

    delete child_token_url(child_id), headers: bearer(secret)

    assert_response :no_content
    introspect(child)
    assert_inactive
  end

  test "the Dispatcher revokes only its own children" do
    _, secret = dispatcher_token
    other_parent, other = dispatcher_token(name: "Old Dispatcher")
    other_child_id, other_child = child_token(other)

    [ other_child_id, other_parent.id, @admin.api_tokens.create!(name: "x", token_digest: "d").id ].each do |id|
      assert_no_difference -> { ApiToken.count } do
        delete child_token_url(id), headers: bearer(secret)
      end
      assert_response :not_found
    end
    introspect(other_child)
    assert_equal true, response.parsed_body["active"]

    _, child = child_token(secret)
    delete child_token_url(other_child_id), headers: bearer(child)
    assert_response :forbidden
  end

  test "revoking the Dispatcher's token on the settings page stops each child at once" do
    parent, secret = dispatcher_token
    children = 2.times.map { child_token(secret).last }

    sign_in_as(@admin)
    delete api_token_url(parent)

    children.each do |child|
      introspect(child)
      assert_inactive
    end
  end

  test "the Dispatcher's token and each child stop working when its Admin leaves the Organization" do
    other_admin = create(:user)
    create(:organization_user, organization: @acme, user: other_admin, role: :admin)
    _, secret = dispatcher_token
    _, child = child_token(secret)

    @admin.organization_users.find_by!(organization: @acme).discard!

    [ secret, child ].each do |token|
      introspect(token)
      assert_inactive
    end
    refused_child(secret, scopes: [ "run_events:write" ], expires_at: 1.hour.from_now.iso8601, status: :unauthorized)
  end

  test "a child whose parent no longer works does not work" do
    parent, secret = dispatcher_token(expires_at: 2.hours.from_now)
    _, child = child_token(secret, expires_at: 2.hours.from_now)
    parent.update_column(:expires_at, 1.hour.from_now)

    travel_to(90.minutes.from_now) do
      introspect(child)
      assert_inactive
    end
  end

  # The settings page

  test "the page lists the Dispatcher's token with its live children's count, not each child" do
    _, secret = dispatcher_token
    child_token(secret)
    child_token(secret, expires_at: 1.hour.from_now)

    sign_in_as(@admin)
    travel_to(2.hours.from_now) do
      listed = page_props["api_tokens"]
      assert_equal [ "Dispatcher" ], listed.pluck("name")
      assert_equal 1, listed.sole["children"]
    end
  end

  private

  def dispatcher_token(name: "Dispatcher", expires_at: @parent_expiry)
    ApiToken.issue!(user: @admin, name: name, scopes: DISPATCHER_SCOPES, organization_id: @acme.id, expires_at: expires_at)
  end

  # A child with run_events:write; its id and secret.
  def child_token(parent_secret, expires_at: 3.hours.from_now)
    make_child(parent_secret, scopes: [ "run_events:write" ], expires_at: expires_at.iso8601)
    assert_response :created
    [ response.parsed_body.dig("api_token", "id"), response.parsed_body["secret"] ]
  end

  def make_child(secret, name: "Execution", **fields)
    post child_tokens_url, params: { name: name, **fields }, headers: bearer(secret), as: :json
  end

  def refused_child(secret, status: :unprocessable_content, **fields)
    assert_no_difference -> { ApiToken.count } do
      make_child(secret, **fields)
    end
    assert_response status
  end

  def bearer(secret)
    secret ? { "Authorization" => "Bearer #{secret}" } : {}
  end

  def make_on_page(name: "Dispatcher", **fields)
    post api_tokens_url, params: { api_token: { name: name, **fields } }, as: :json
  end

  def refused_on_page(**fields)
    assert_no_difference -> { ApiToken.count } do
      make_on_page(**fields)
    end
    assert_response :unprocessable_content
  end

  def errors
    response.parsed_body["errors"]
  end

  def introspect(token)
    post oauth_introspect_url, params: { token: token }, headers: bearer(@caller)
  end

  def assert_inactive
    assert_response :ok
    assert_equal({ "active" => false }, response.parsed_body)
  end

  def page_props
    get api_token_settings_url(org_slug: @acme.slug), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    response.parsed_body["props"]
  end
end
