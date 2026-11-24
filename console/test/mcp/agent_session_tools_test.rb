# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# list_agent_sessions and get_agent_session: the Agent sessions of an
# Organization's Agent Project, as the Agent sessions page lists them, and
# one Agent session's header and a page of its timeline, as the Agent
# session page shows them. EventStore's answers are stubbed in the shape of
# its POST /api/:project_id/agent-sessions and /agent-sessions/:id.
class McpAgentSessionToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  CART = "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
  RUN = "7d1e5a90-2c4b-4f7a-8e61-5b3c9d0a2e17"

  setup do
    @admin = create(:user, name: "Ada", email: "ada@example.com")
    @member = create(:user, name: "Bob", email: "bob@example.com")
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @admin, role: :admin)
    create(:organization_user, organization: @acme, user: @member, role: :member)
    @agent = create(:project, organization: @acme, name: "Agent sessions", slug: "agent-sessions", agent: true)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    create(:project, organization: create(:organization, slug: "theirs"), slug: "secret")
    travel_to Time.utc(2026, 10, 3, 12, 0, 0)
  end

  # list_agent_sessions

  test "list_agent_sessions lists the last 7 days' Agent sessions with the person or the Run, the totals, and links" do
    asked = stub_list(from: "2026-09-26T12:00:00.000Z", to: "2026-10-03T12:00:00.000Z")

    got = call_tool(:list_agent_sessions, user: @member)

    assert_not got.error?
    assert_requested asked
    run, own = got.json["agent_sessions"]
    assert_equal [ RUN, { "id" => "run_42", "project" => "shop" }, { "id" => @admin.id, "name" => "Ada" } ],
      run.values_at("id", "run", "person")
    assert_equal [ CART, nil, { "id" => @member.id, "name" => "Bob" }, 0.00174, 1000, "claude-opus-5-5", "high" ],
      own.values_at("id", "run", "person", "cost_usd", "cache_read_tokens", "model", "effort")
    assert_not own.key?("user_id")
    assert_equal [ "", "Cart page" ], [ run["title"], own["title"] ]
    assert_equal "http://console.test/acme/agent_sessions/#{CART}", own["link"]
    assert_equal({ "agent_sessions" => 2, "input_tokens" => 20, "output_tokens" => 100, "cache_read_tokens" => 2000,
                   "cache_write_tokens" => 200, "cost_usd" => 0.00174, "no_cost" => 1 }, got.json["totals"])
    assert_equal false, got.json["more"]
    assert_equal "estimate at list prices", got.json["cost_label"]
    assert_equal [ "github.com/acme/shop" ], got.json["repositories"]
    assert_equal "http://console.test/acme/agent_sessions?range=7d", got.link
  end

  test "list_agent_sessions passes the page's filters and the range, and keeps them in the link" do
    asked = stub_list(from: "2026-10-02T12:00:00.000Z", to: "2026-10-03T12:00:00.000Z", user_id: @member.id,
                      repository: "github.com/acme/shop", project: "shop", run: "run")

    got = call_tool(:list_agent_sessions, user: @member, range: "24h", person: "bob@example.com",
                    repository: "github.com/acme/shop", project: "shop", run: "run")

    assert_not got.error?
    assert_requested asked
    assert_equal({ "person" => @member.id, "repository" => "github.com/acme/shop", "project" => "shop", "run" => "run" },
      got.json["filters"])
    assert_equal "http://console.test/acme/agent_sessions?person=#{@member.id}&project=shop&range=24h" \
                 "&repository=github.com%2Facme%2Fshop&run=run", got.link
  end

  test "list_agent_sessions passes the sort and the direction, and keeps them in the link" do
    asked = stub_list(from: "2026-09-26T12:00:00.000Z", to: "2026-10-03T12:00:00.000Z", sort: "cost", direction: "desc")

    got = call_tool(:list_agent_sessions, user: @member, sort: "cost", direction: "desc")

    assert_not got.error?
    assert_requested asked
    assert_equal({ "sort" => "cost", "direction" => "desc" }, got.json["order"])
    assert_equal "http://console.test/acme/agent_sessions?direction=desc&range=7d&sort=cost", got.link
  end

  test "list_agent_sessions refuses a sort or a direction that is not one" do
    assert call_tool(:list_agent_sessions, user: @member, sort: "title").error?
    assert call_tool(:list_agent_sessions, user: @member, direction: "up").error?
  end

  test "list_agent_sessions tells the caller how to sort" do
    description = listed_tools(user: @member).find { |tool| tool[:name] == "list_agent_sessions" }[:description]

    assert_match(/sort/, description)
    assert_match(/most expensive/, description)
  end

  test "both tools name the effort next to the model" do
    tools = listed_tools(user: @member).index_by { |tool| tool[:name] }

    assert_match(/model, effort/, tools["list_agent_sessions"][:description])
    assert_match(/model, effort/, tools["get_agent_session"][:description])
  end

  test "list_agent_sessions takes a person by user id too" do
    asked = stub_list(from: "2026-09-26T12:00:00.000Z", to: "2026-10-03T12:00:00.000Z", user_id: @admin.id)

    assert_not call_tool(:list_agent_sessions, user: @member, person: @admin.id.to_s).error?
    assert_requested asked
  end

  test "list_agent_sessions refuses a person who is not a member, and a Project that is not one of Runs here" do
    stranger = create(:user, email: "eve@example.com")

    assert_match "No member eve@example.com", call_tool(:list_agent_sessions, user: @member, person: "eve@example.com").text
    assert_match "No member #{stranger.id}", call_tool(:list_agent_sessions, user: @member, person: stranger.id.to_s).text
    assert_match "No Project secret", call_tool(:list_agent_sessions, user: @member, project: "secret").text
    assert_match "No Project agent-sessions", call_tool(:list_agent_sessions, user: @member, project: "agent-sessions").text
    assert call_tool(:list_agent_sessions, user: @member, run: "maybe").error?
  end

  test "list_agent_sessions says when some Agent sessions were left out of the rows" do
    stub_list(from: "2026-09-26T12:00:00.000Z", to: "2026-10-03T12:00:00.000Z", more: true)

    got = call_tool(:list_agent_sessions, user: @member)

    assert_not got.error?
    assert_equal true, got.json["more"]
  end

  test "list_agent_sessions asks for 50 Agent sessions, or the limit given, at most 200" do
    first = stub_list(from: "2026-09-26T12:00:00.000Z", to: "2026-10-03T12:00:00.000Z", limit: 50)

    assert_not call_tool(:list_agent_sessions, user: @member).error?
    assert_requested first

    given = stub_list(from: "2026-09-26T12:00:00.000Z", to: "2026-10-03T12:00:00.000Z", limit: 200)
    assert_not call_tool(:list_agent_sessions, user: @member, limit: 200).error?
    assert_requested given

    assert call_tool(:list_agent_sessions, user: @member, limit: 201).error?
    assert call_tool(:list_agent_sessions, user: @member, limit: 0).error?
  end

  test "list_agent_sessions tells the caller how to read the Agent sessions that the limit left out" do
    description = listed_tools(user: @member).find { |tool| tool[:name] == "list_agent_sessions" }[:description]

    assert_match(/more is true/, description)
    assert_match(/narrow the range, or filter by person or repository/, description)
  end

  test "list_agent_sessions leaves the fields with no value out of each row" do
    stub_list(from: "2026-09-26T12:00:00.000Z", to: "2026-10-03T12:00:00.000Z")

    run, own = call_tool(:list_agent_sessions, user: @member).json["agent_sessions"]

    assert_not own.key?("run")
    assert_not run.key?("cost_usd")
    [ run, own ].each do |row|
      assert_equal [], %w[folder parent_session_id flags].select { |name| row.key?(name) }
    end
    assert_not run.key?("effort"), "an empty effort is left out, as an empty model is"
    assert_equal [ "", "Cart page" ], [ run["title"], own["title"] ], "the title stays, also when it is empty"
    assert_equal [ 0, 0 ], [ run["subagents"], own["subagents"] ], "a number stays, also when it is 0"
  end

  test "list_agent_sessions says so when EventStore cannot answer" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/agent-sessions").to_return(status: 500, body: "boom")

    got = call_tool(:list_agent_sessions, user: @member)

    assert got.error?
    assert_match "EventStore can't read the Agent sessions", got.text
  end

  # get_agent_session

  test "get_agent_session answers the header, the subagents and one page of the timeline, and links to its page" do
    asked = stub_session(body: { limit: 50 })

    got = call_tool(:get_agent_session, user: @member, agent_session: CART)

    assert_not got.error?
    assert_requested asked
    session = got.json["agent_session"]
    assert_equal [ CART, "Cart page", "apps/web", { "id" => @member.id, "name" => "Bob" }, nil ],
      session.values_at("id", "title", "folder", "person", "run")
    assert_equal [ "a1" ], got.json["subagents"].pluck("agent_id")
    assert_equal [ "Add a cart page", "Done.", "cart.go" ], got.json["steps"].pluck("text")
    assert_equal [ "claude-opus-5-5", "high" ], got.json["steps"].second.values_at("model", "effort")
    assert_equal "high", session["effort"]
    assert_equal({ "kind" => "person", "name" => "Bob" }, got.json["steps"].first["from"])
    assert_equal "c2", got.json["next_cursor"]
    assert_equal "estimate at list prices", got.json["cost_label"]
    assert_equal "http://console.test/acme/agent_sessions/#{CART}", got.link
  end

  test "get_agent_session reads the next page of a subagent's transcript, and searches with a text" do
    asked = stub_session(body: { path: "subagents/agent-a1.jsonl", cursor: "c2", limit: 200 })
    found = stub_session(body: { search: "cart", limit: 50 })

    assert_not call_tool(:get_agent_session, user: @member, agent_session: CART, path: "subagents/agent-a1.jsonl",
                                             cursor: "c2", limit: 200).error?
    assert_not call_tool(:get_agent_session, user: @member, agent_session: CART, search: "cart").error?

    assert_requested asked
    assert_requested found
  end

  test "get_agent_session says when the Agent Project has no such Agent session" do
    stub_session(status: 404)

    got = call_tool(:get_agent_session, user: @member, agent_session: CART)

    assert got.error?
    assert_match "No Agent session #{CART} in acme. list_agent_sessions lists them.", got.text
    assert_match "is not an Agent session id", call_tool(:get_agent_session, user: @member, agent_session: "../runs").text
  end

  test "get_agent_session says so when EventStore cannot answer" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/agent-sessions/#{CART}").to_return(status: 500, body: "boom")

    got = call_tool(:get_agent_session, user: @member, agent_session: CART)

    assert got.error?
    assert_match "Try again shortly", got.text
  end

  # Both

  test "a user who is not a member of the Organization gets an error, and EventStore is not asked" do
    outsider = create(:user)
    create(:organization_user, organization: Organization.find_by!(slug: "theirs"), user: outsider)

    [ call_tool(:list_agent_sessions, user: outsider, organization: "acme"),
      call_tool(:get_agent_session, user: outsider, organization: "acme", agent_session: CART) ].each do |got|
      assert got.error?
      assert_match "No Organization acme among yours", got.text
    end
    assert_not_requested :post, %r{/agent-sessions}
  end

  test "both tools say so when the Organization has no Agent Project" do
    @agent.destroy!

    [ call_tool(:list_agent_sessions, user: @member), call_tool(:get_agent_session, user: @member, agent_session: CART) ].each do |got|
      assert got.error?
      assert_match "Agent sessions are off in acme", got.text
    end
  end

  private

  def row(id, user_id:, cost:, run_id: "", project: "", folder: "", effort: "high")
    {
      title: id == CART ? "Cart page" : "",
      id: id, first: "2026-10-02T11:00:00Z", last: "2026-10-02T11:00:06Z", user_id: user_id, harness: "claude-code",
      repository: "github.com/acme/shop", folder: folder, run_id: run_id, project: project, parent_session_id: "",
      model: "claude-opus-5-5", effort: effort, input_tokens: 10, output_tokens: 50, cache_read_tokens: 1000, cache_write_tokens: 100,
      cost_usd: cost, person_messages: 1, turns: 1, tool_calls: 1, subagents: 0, flags: [],
    }
  end

  def stub_list(from:, to:, more: false, **filters)
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/agent-sessions")
      .with(body: hash_including({ from: from, to: to }.merge(filters).stringify_keys))
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: {
        agent_sessions: [ row(RUN, user_id: @admin.id, cost: nil, run_id: "run_42", project: "shop", effort: ""),
                          row(CART, user_id: @member.id, cost: 0.00174) ],
        totals: { agent_sessions: 2, input_tokens: 20, output_tokens: 100, cache_read_tokens: 2000, cache_write_tokens: 200,
                  cost_usd: 0.00174, no_cost: 1 },
        more: more, repositories: [ "github.com/acme/shop" ],
      }.to_json)
  end

  def step(role, text, **more)
    { id: SecureRandom.uuid, time: "2026-10-02T11:00:00Z", path: "session.jsonl", version: 1, offset: 0, line: "user",
      role: role, text: text, tokens: nil, cost_usd: nil, cut: false }.merge(more)
  end

  def stub_session(body: {}, status: 200)
    answer = {
      agent_session: row(CART, user_id: @member.id, cost: 0.01, folder: "apps/web"),
      subagents: [ { path: "subagents/agent-a1.jsonl", agent_id: "a1", tool_use_id: "toolu_9",
                     first: "2026-10-02T11:00:02Z", last: "2026-10-02T11:00:03Z" } ],
      steps: [ step("person", "Add a cart page"),
               step("assistant", "Done.", line: "assistant/text", model: "claude-opus-5-5", effort: "high"),
               step("tool", "cart.go", tool_name: "Agent", tool_use_id: "toolu_9") ],
      next_cursor: "c2",
    }
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/agent-sessions/#{CART}")
      .with(body: hash_including(body.stringify_keys))
      .to_return(status: status, headers: { "Content-Type" => "application/json" },
                 body: status == 200 ? answer.to_json : { error: "no such Agent session" }.to_json)
  end
end
