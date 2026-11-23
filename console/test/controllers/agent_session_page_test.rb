# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Agent session page: the header of one Agent session of the
# Organization's Agent Project and its timeline, from EventStore's
# /agent-sessions/:id, with the next pages and each subagent's timeline as
# JSON. Each member sees it; someone outside the Organization, or an Agent
# session that the Agent Project does not have, gets 404.
class AgentSessionPageTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  CART = "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
  PARENT = "9e1f3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"

  setup do
    @organization = create(:organization, slug: "acme")
    @admin = create(:user, name: "Ada")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user, name: "Bob")
    create(:organization_user, organization: @organization, user: @member, role: :member)
    travel_to Time.utc(2026, 10, 3, 12, 0, 0)
    @project = AgentSessionsSettings.turn_on(organization: @organization, user: @admin, entries: [ "github.com/acme/shop" ], confirmed: "1").project
  end

  def header(user_id:, run_id: "", project: "", flags: [])
    {
      title: "Cart page",
      id: CART, first: "2026-10-02T11:00:00Z", last: "2026-10-02T11:00:09Z", user_id: user_id, harness: "claude-code",
      repository: "github.com/acme/shop", folder: "apps/web", run_id: run_id, project: project, parent_session_id: PARENT,
      model: "claude-opus-5-5", input_tokens: 20, output_tokens: 100, cache_read_tokens: 2000, cache_write_tokens: 200,
      cost_usd: 0.01, person_messages: 1, turns: 1, tool_calls: 1, subagents: 1, flags: flags,
    }
  end

  def step(role, text, **more)
    { id: SecureRandom.uuid, time: "2026-10-02T11:00:00Z", path: "session.jsonl", version: 1, offset: 0, line: "user",
      role: role, text: text, tokens: nil, cost_usd: nil, cut: false }.merge(more)
  end

  def stub_session(body: {}, status: 200, **header_options)
    answer = {
      agent_session: header(**header_options),
      subagents: [ { path: "subagents/agent-a1.jsonl", agent_id: "a1", tool_use_id: "toolu_9", first: "2026-10-02T11:00:02Z", last: "2026-10-02T11:00:03Z" } ],
      steps: [
        step("person", "Add a cart page"),
        step("assistant", '{"description":"Find the cart"}', line: "assistant/tool_use", tool_name: "Agent", tool_use_id: "toolu_9"),
        step("tool", "cart.go", line: "user/tool_result", tool_name: "Agent", tool_use_id: "toolu_9", subagent_id: "a1"),
        step("not from a person", "Stop hook feedback: run the report"),
      ],
      next_cursor: "c2",
    }
    stub_request(:post, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{CART}")
      .with(body: hash_including(body))
      .to_return(status: status, headers: { "Content-Type" => "application/json" }, body: status == 200 ? answer.to_json : { error: "no such Agent session" }.to_json)
  end

  def page(id = CART)
    get agent_session_path(org_slug: "acme", id: id), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end

  def props
    assert_response :success
    assert_equal "AgentSessions/Show", response.parsed_body["component"]
    response.parsed_body.dig("props", "agent_session")
  end

  test "a member sees the header and the timeline of an engineer's Agent session, each person message from that engineer" do
    stub_session(user_id: @admin.id)
    sign_in_as(@member)
    page
    view = props

    header = view["agent_session"]
    assert_equal({ "id" => @admin.id, "name" => "Ada" }, header["person"])
    assert_nil header["run"]
    assert_equal [ "github.com/acme/shop", "apps/web", PARENT, "claude-opus-5-5", 0.01 ],
      header.values_at("repository", "folder", "parent_session_id", "model", "cost_usd")
    assert_equal "Cart page", header["title"]
    assert_equal "estimate at list prices", view["cost_label"]
    assert_nil view["resume_warning"]

    steps = view["steps"]
    assert_equal [ "Ada", nil, nil, nil ], steps.map { |s| s.dig("from", "name") }
    assert_equal [ "person", "agent", "tool", "not from a person" ], steps.map { |s| s.dig("from", "kind") }
    assert_equal "c2", view["next_cursor"]
    assert_equal [ { "path" => "subagents/agent-a1.jsonl", "agent_id" => "a1", "tool_use_id" => "toolu_9",
                     "first" => "2026-10-02T11:00:02Z", "last" => "2026-10-02T11:00:03Z" } ], view["subagents"]
    assert_equal "/acme/agent_sessions/#{CART}/timeline", view["timeline_path"]
    assert_equal "/acme/agent_sessions/#{CART}/line", view["line_path"]
    assert_equal "/acme/agent_sessions/#{PARENT}", view["parent_path"]
  end

  test "a Run's Agent session names its Run, and its person messages wait for the Run's events" do
    stub_session(user_id: @admin.id, run_id: "run_42", project: "shop")
    sign_in_as(@member)
    page
    view = props

    assert_equal({ "id" => "run_42", "project" => "shop" }, view.dig("agent_session", "run"))
    assert_equal({ "kind" => "person", "name" => nil }, view["steps"].first["from"])
  end

  test "a cut or partial record shows its flag with the text that a resume can fail" do
    stub_session(user_id: @admin.id, flags: [ "cut", "partial" ])
    sign_in_as(@member)
    page
    view = props

    assert_equal [ "cut", "partial" ], view.dig("agent_session", "flags")
    assert_match(/a resume of this Agent session can fail/i, view["resume_warning"])
  end

  test "each flag that makes a resume fail gives the warning, and a trimmed record does not" do
    { [ "cut" ] => true, [ "partial" ] => true, [ "trimmed" ] => false }.each do |flags, warns|
      stub_session(user_id: @admin.id, flags: flags)
      sign_in_as(@member)
      page
      warning = props["resume_warning"]
      if warns
        assert_match(/a resume of this Agent session can fail/i, warning, "flags #{flags}")
      else
        assert_nil warning, "flags #{flags}"
      end
    end
  end

  test "the next page and a subagent's timeline come as JSON" do
    stub_session(body: { "path" => "subagents/agent-a1.jsonl", "cursor" => "c1" }, user_id: @member.id)
    sign_in_as(@member)
    get agent_session_timeline_path(org_slug: "acme", id: CART, path: "subagents/agent-a1.jsonl", cursor: "c1"), as: :json

    assert_response :success
    assert_equal [ "Bob", nil, nil, nil ], response.parsed_body["steps"].map { |s| s.dig("from", "name") }
    assert_equal "c2", response.parsed_body["next_cursor"]
  end

  test "the newest order goes to EventStore, and an order that is not one is not sent" do
    stub_session(user_id: @member.id)
    sign_in_as(@member)
    url = "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{CART}"
    get agent_session_timeline_path(org_slug: "acme", id: CART, order: "newest", cursor: "c1"), as: :json

    assert_response :success
    assert_requested :post, url, body: hash_including("order" => "newest", "cursor" => "c1")

    get agent_session_timeline_path(org_slug: "acme", id: CART, order: "random"), as: :json
    assert_response :success
    assert_requested(:post, url) { |request| !JSON.parse(request.body).key?("order") }
  end

  test "a search inside the Agent session goes to EventStore and gives its matching steps as JSON" do
    stub_session(body: { "search" => "cart" }, user_id: @member.id)
    sign_in_as(@member)
    get agent_session_timeline_path(org_slug: "acme", id: CART, search: "cart"), as: :json

    assert_response :success
    assert_equal "Add a cart page", response.parsed_body["steps"].first["text"]
    assert_requested :post, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{CART}", body: hash_including("search" => "cart")
  end

  def stub_line(status: 200, project: @project)
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/agent-sessions/#{CART}/line")
      .with(body: { path: "session.jsonl", version: 1, offset: 120 }.to_json)
      .to_return(status: status, headers: { "Content-Type" => "application/json" },
                 body: status == 200 ? { line: '{"type":"user","message":"all of it"}', cut: false }.to_json : { error: "no such line" }.to_json)
  end

  def line(org_slug: "acme", version: 1, offset: 120)
    get agent_session_line_path(org_slug: org_slug, id: CART, path: "session.jsonl", version: version, offset: offset), as: :json
  end

  test "'Show full' gives the raw line of a step as JSON" do
    stub_line
    sign_in_as(@member)
    line

    assert_response :success
    assert_equal({ "line" => '{"type":"user","message":"all of it"}', "cut" => false }, response.parsed_body)
  end

  test "a raw line that the Agent Project does not have, or of another Organization's Agent Project, is 404" do
    stub_line(status: 404)
    sign_in_as(@member)
    line
    assert_response :not_found

    other = create(:organization, slug: "other")
    create(:organization_user, organization: other, user: @admin, role: :admin)
    other_project = AgentSessionsSettings.turn_on(organization: other, user: @admin, entries: [ "github.com/other/app" ], confirmed: "1").project
    stub_line(project: other_project)
    line(org_slug: "other")
    assert_response :not_found
    assert_not_requested :post, "#{EventStore.base_url}/api/#{other_project.id}/agent-sessions/#{CART}/line"
  end

  test "a raw line with a negative or non-numeric version or offset is 404 and does not go to EventStore" do
    stub_line
    sign_in_as(@member)
    [ { version: -1 }, { offset: -1 }, { version: "abc" }, { offset: "1x" } ].each do |bad|
      line(**bad)
      assert_response :not_found, "for #{bad}"
    end
    assert_not_requested :post, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{CART}/line"
  end

  test "EventStore down gives 503 on 'Show full'" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{CART}/line").to_return(status: 500, body: "boom")
    sign_in_as(@member)
    line

    assert_response :service_unavailable
    assert response.parsed_body["error"].present?
  end

  test "an Agent session that the Agent Project does not have is 404" do
    stub_session(status: 404, user_id: @admin.id)
    sign_in_as(@member)
    page
    assert_response :not_found

    get agent_session_timeline_path(org_slug: "acme", id: CART), as: :json
    assert_response :not_found
  end

  test "someone outside the Organization gets 404" do
    stub_session(user_id: @admin.id)
    sign_in_as(create(:user))
    page
    assert_response :not_found
  end

  test "each Organization with an Agent Project reads the Agent sessions of its own, never another Organization's" do
    own = stub_session(user_id: @admin.id)
    own_line = stub_line
    other = create(:organization, slug: "other")
    other_admin = create(:user, name: "Olga")
    create(:organization_user, organization: other, user: other_admin, role: :admin)
    other_project = AgentSessionsSettings.turn_on(organization: other, user: other_admin, entries: [ "github.com/other/app" ], confirmed: "1").project
    asked = stub_request(:post, "#{EventStore.base_url}/api/#{other_project.id}/agent-sessions/#{CART}")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" },
                 body: { agent_session: header(user_id: other_admin.id), subagents: [], steps: [ step("person", "Other's step") ], next_cursor: nil }.to_json)
    asked_line = stub_request(:post, "#{EventStore.base_url}/api/#{other_project.id}/agent-sessions/#{CART}/line")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { line: "the other's line", cut: false }.to_json)
    sign_in_as(other_admin)

    get agent_session_path(org_slug: "other", id: CART), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal [ "Other's step" ], response.parsed_body.dig("props", "agent_session", "steps").map { |s| s["text"] }

    get agent_session_timeline_path(org_slug: "other", id: CART), as: :json
    assert_response :success
    get agent_session_line_path(org_slug: "other", id: CART, path: "session.jsonl", version: 1, offset: 120), as: :json
    assert_response :success
    assert_equal "the other's line", response.parsed_body["line"]

    assert_requested asked, times: 2
    assert_requested asked_line
    assert_not_requested own
    assert_not_requested own_line
  end

  test "a raw line with no path, or a blank one, is 404 and does not go to EventStore" do
    stub_line
    sign_in_as(@member)
    get agent_session_line_path(org_slug: "acme", id: CART, version: 1, offset: 120), as: :json
    assert_response :not_found
    get agent_session_line_path(org_slug: "acme", id: CART, path: " ", version: 1, offset: 120), as: :json
    assert_response :not_found
    assert_not_requested :post, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{CART}/line"
  end

  test "EventStore down shows the page with the error" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{CART}").to_return(status: 500, body: "boom")
    sign_in_as(@member)
    page
    view = props

    assert_equal false, view.dig("event_store", "available")
    assert_nil view["agent_session"]
    assert_equal [], view["steps"]
  end

  test "EventStore down gives 503 with the error on the JSON timeline, not an empty last page" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{CART}").to_return(status: 500, body: "boom")
    sign_in_as(@member)
    get agent_session_timeline_path(org_slug: "acme", id: CART, cursor: "c1"), as: :json

    assert_response :service_unavailable
    assert response.parsed_body["error"].present?
    assert_nil response.parsed_body["next_cursor"]
  end
end
