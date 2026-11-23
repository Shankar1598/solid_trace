# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Agent sessions page (Organization navigation): the Agent sessions of
# the Organization's Agent Project that the filters in the URL select, with
# the totals of their tokens and estimated cost, from EventStore's
# /agent-sessions. Each member sees it; someone outside the Organization
# gets 404; with Agent sessions off, it tells how to turn them on.
class AgentSessionsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  CART = "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
  RUN = "7d1e5a90-2c4b-4f7a-8e61-5b3c9d0a2e17"

  setup do
    @organization = create(:organization, slug: "acme")
    @admin = create(:user, name: "Ada")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user, name: "Bob")
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @shop = create(:project, organization: @organization, name: "Shop", slug: "shop")
    travel_to Time.utc(2026, 10, 3, 12, 0, 0)
  end

  def turn_on
    AgentSessionsSettings.turn_on(organization: @organization, user: @admin, entries: [ "github.com/acme/shop" ], confirmed: "1").project
  end

  def row(id, user_id:, cost:, run_id: "", project: "", last: "2026-10-02T11:00:06Z", title: nil)
    {
      title: title,
      id: id, first: "2026-10-02T11:00:00Z", last: last, user_id: user_id, harness: "claude-code",
      repository: "github.com/acme/shop", folder: "", run_id: run_id, project: project, parent_session_id: "",
      model: "claude-opus-5-5", input_tokens: 10, output_tokens: 50, cache_read_tokens: 1000, cache_write_tokens: 100,
      cost_usd: cost, person_messages: 1, turns: 1, tool_calls: 1, subagents: 0, flags: [],
    }
  end

  def stub_agent_sessions(project, body)
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/agent-sessions")
      .with(body: hash_including(body))
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: {
        agent_sessions: [ row(RUN, user_id: @admin.id, cost: nil, run_id: "run_42", project: "shop", last: "2026-10-03T10:00:00Z",
                              title: "Fix issue 42"),
                          row(CART, user_id: @member.id, cost: 0.00174).except(:title) ],
        totals: { agent_sessions: 2, input_tokens: 20, output_tokens: 100, cache_read_tokens: 2000, cache_write_tokens: 200,
                  cost_usd: 0.00174, no_cost: 1 },
        more: false, repositories: [ "github.com/acme/shop" ],
      }.to_json)
  end

  def page(params = {})
    get agent_sessions_path(org_slug: "acme", **params), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "AgentSessions/Index", response.parsed_body["component"]
    response.parsed_body.dig("props", "agent_sessions")
  end

  test "a member sees the Agent sessions of the last 7 days, with the person or the Run, and the totals" do
    project = turn_on
    asked = stub_agent_sessions(project, { "from" => "2026-09-26T12:00:00.000Z", "to" => "2026-10-03T12:00:00.000Z" })
    sign_in_as(@member)

    view = page

    assert_requested asked
    assert view["on"]
    assert_equal "7d", view.dig("range", "span")
    run, cart = view["rows"]
    assert_equal [ RUN, CART ], [ run["id"], cart["id"] ]
    assert_equal "Bob", cart.dig("person", "name")
    assert_nil cart["run"]
    assert_equal({ "id" => "run_42", "project" => "shop" }, run["run"])
    assert_equal 0.00174, cart["cost_usd"]
    assert_nil run["cost_usd"]
    assert_equal [ 10, 50, 1000, 100 ], cart.values_at("input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens")
    assert_equal [ 1, 1 ], cart.values_at("person_messages", "tool_calls")
    # The title of an Agent session, and "" from an EventStore that gives
    # none (an EventStore older than Console).
    assert_equal [ "Fix issue 42", "" ], [ run["title"], cart["title"] ]
    assert_equal({ "agent_sessions" => 2, "input_tokens" => 20, "output_tokens" => 100, "cache_read_tokens" => 2000,
                   "cache_write_tokens" => 200, "cost_usd" => 0.00174, "no_cost" => 1 }, view["totals"])
    assert_equal "estimate at list prices", view["cost_label"]
    assert view.dig("event_store", "available")
  end

  test "the filters in the URL go to EventStore and come back on the page, with the choices of each" do
    project = turn_on
    asked = stub_agent_sessions(project, {
      "from" => "2026-10-02T09:00:00.000Z", "to" => "2026-10-02T10:00:00.000Z", "user_id" => @member.id,
      "repository" => "github.com/acme/api", "project" => "shop", "run" => "run",
    })
    sign_in_as(@member)

    view = page(from: "2026-10-02T09:00:00Z", to: "2026-10-02T10:00:00Z", person: @member.id,
      repository: "github.com/acme/api", project: "shop", run: "run")

    assert_requested asked
    assert_equal({ "person" => @member.id, "repository" => "github.com/acme/api", "project" => "shop", "run" => "run" }, view["filters"])
    assert_equal [ nil, "2026-10-02T09:00:00.000Z", "2026-10-02T10:00:00.000Z" ], view["range"].values_at("span", "from", "to")
    assert_equal [ "Ada", "Bob" ], view["people"].map { |p| p["name"] }
    # The Projects of Runs: not the Agent Project itself.
    assert_equal [ { "name" => "Shop", "slug" => "shop" } ], view["projects"]
    # The repository of the filter stays a choice when the range has none of it.
    assert_equal [ "github.com/acme/api", "github.com/acme/shop" ], view["repositories"]
  end

  test "a filter that is not a choice is not sent, and a range that cannot be read is the default" do
    project = turn_on
    asked = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/agent-sessions")
      .with { |request| (JSON.parse(request.body).keys - %w[from to limit]).empty? && JSON.parse(request.body)["from"] == "2026-09-26T12:00:00.000Z" }
      .to_return(status: 200, headers: { "Content-Type" => "application/json" },
        body: { agent_sessions: [], totals: { agent_sessions: 0, no_cost: 0 }, more: false, repositories: [] }.to_json)
    sign_in_as(@member)

    view = page(range: "forever", person: "someone", run: "maybe")

    assert_requested asked
    assert_equal({ "person" => nil, "repository" => nil, "project" => nil, "run" => nil }, view["filters"])
    assert_equal "7d", view.dig("range", "span")
  end

  test "the person of an Agent session who is not a member any more still has a name" do
    project = turn_on
    stub_agent_sessions(project, {})
    @organization.organization_users.find_by(user: @member).destroy
    sign_in_as(@admin)

    assert_equal "Bob", page["rows"].last.dig("person", "name")
  end

  test "when EventStore cannot answer, the page says why and has no rows" do
    project = turn_on
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/agent-sessions").to_return(status: 500, body: "boom")
    sign_in_as(@member)

    view = page

    assert_not view.dig("event_store", "available")
    assert_match "Agent sessions", view.dig("event_store", "error")
    assert_equal [], view["rows"]
  end

  test "with Agent sessions off, the page tells how to turn them on and reads nothing" do
    sign_in_as(@member)

    view = page

    assert_not view["on"]
    assert_not view["can_turn_on"]
    assert_equal "/acme/settings/agent_sessions", view["settings_path"]
    assert_not_requested :post, /agent-sessions/

    sign_in_as(@admin)
    assert page["can_turn_on"]
  end

  test "someone outside the Organization gets 404" do
    turn_on
    sign_in_as(create(:user))

    get agent_sessions_path(org_slug: "acme")

    assert_response :not_found
  end

  test "each Organization with an Agent Project reads the Agent sessions of its own, never another Organization's" do
    acme_asked = stub_agent_sessions(turn_on, {})
    other = create(:organization, slug: "other")
    other_admin = create(:user)
    create(:organization_user, organization: other, user: other_admin, role: :admin)
    other_project = AgentSessionsSettings.turn_on(organization: other, user: other_admin, entries: [ "github.com/other/app" ], confirmed: "1").project
    other_asked = stub_request(:post, "#{EventStore.base_url}/api/#{other_project.id}/agent-sessions")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: {
        agent_sessions: [ row(CART, user_id: other_admin.id, cost: nil) ],
        totals: { agent_sessions: 1, input_tokens: 10, output_tokens: 50, cache_read_tokens: 0, cache_write_tokens: 0, cost_usd: nil, no_cost: 1 },
        more: false, repositories: [ "github.com/other/app" ],
      }.to_json)
    sign_in_as(other_admin)

    get agent_sessions_path(org_slug: "other"), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }

    assert_response :success
    assert_equal [ CART ], response.parsed_body.dig("props", "agent_sessions", "rows").map { |r| r["id"] }
    assert_requested other_asked
    assert_not_requested acme_asked
  end

  test "search across Agent sessions opens the Logs explorer on the Agent Project with 7 days, which a member can read" do
    project = turn_on
    stub_agent_sessions(project, {})
    sign_in_as(@member)

    search = page["search"]

    assert_equal({ "logs_path" => "/acme/projects/#{project.slug}/logs", "project" => project.slug, "range" => "7d" }, search)
    get search["logs_path"], params: { range: "30d", q: "search \"cart\"" }, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "Logs/Index", response.parsed_body["component"]
    assert_equal project.slug, response.parsed_body.dig("props", "project", "slug")
  end
end
