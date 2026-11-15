# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# list_runs and get_run: the Runs of an Organization and one Run, read from
# the run events in its Agent Project, as the Runs page and the Run page
# show them. EventStore's answers are stubbed in the shape of its POST
# /api/:project_id/runs and GET /api/:project_id/runs/:run_id.
class McpRunToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @agent = create(:project, organization: @acme, name: "Agent sessions", slug: "agent-sessions", agent: true)
    @checkout = create(:project, organization: @acme, name: "Checkout", slug: "checkout")
    create(:project, organization: create(:organization, slug: "theirs"), slug: "secret")
    travel_to Time.utc(2026, 11, 15, 12, 0, 0)
  end

  test "list_runs lists the Runs of the last 30 days with state, Project and PR, and links to the Runs page" do
    asked = stub_runs(from: "2026-10-16T12:00:00.000Z", to: "2026-11-15T12:00:00.000Z",
                      runs: [ summary("r1", state: "waiting_on_person", reason: "needs a person", active: true),
                              summary("r2", state: "done", pr: { "url" => "https://bitbucket.org/acme/shop/pull-requests/9", "outcome" => "merged" },
                                            closed: true) ])

    got = call_tool(:list_runs, user: @user)

    assert_not got.error?
    assert_requested asked
    first, second = got.json["runs"]
    assert_equal [ "r1", "waiting_on_person", "needs a person", "checkout", true ],
      first.values_at("id", "state", "reason", "project", "active")
    assert_equal({ "url" => "https://bitbucket.org/acme/shop/pull-requests/9", "outcome" => "merged" }, second["pr"])
    assert_equal [ "Fix the cart total", "cli", "bitbucket.org/acme/shop", "web", { "kind" => "person", "id" => "7", "name" => "Ana" }, 1 ],
      first.values_at("title", "trigger", "repository", "folder", "actor", "executions")
    assert_equal "2026-11-15T11:00:00.000Z", first["last_event_at"]
    assert_equal "http://console.test/acme/runs/r1", first["link"]
    assert_equal false, got.json["more"]
    assert_equal "http://console.test/acme/runs?range=30d", got.link
  end

  test "list_runs passes the Project and state filters and the range, and keeps them in the link" do
    asked = stub_runs(from: "2026-11-08T12:00:00.000Z", to: "2026-11-15T12:00:00.000Z", project: "checkout",
                      state: "waiting_on_person", limit: 20, runs: [], more: true)

    got = call_tool(:list_runs, user: @user, project: "checkout", state: "waiting_on_person", range: "7d", limit: 20)

    assert_not got.error?
    assert_requested asked
    assert_equal [], got.json["runs"]
    assert_equal true, got.json["more"]
    assert_equal "http://console.test/acme/runs?project=checkout&range=7d&state=waiting_on_person", got.link
  end

  test "list_runs refuses a Project outside the Organization, the Agent Project, and a state that is not one" do
    assert_match "No Project secret", call_tool(:list_runs, user: @user, project: "secret").text
    assert_match "No Project agent-sessions", call_tool(:list_runs, user: @user, project: "agent-sessions").text
    assert call_tool(:list_runs, user: @user, state: "closed").error?
  end

  test "both tools say so when the Organization has no Agent Project" do
    @agent.destroy!

    [ call_tool(:list_runs, user: @user), call_tool(:get_run, user: @user, run: "r1") ].each do |got|
      assert got.error?
      assert_match "Agent sessions are off", got.text
    end
  end

  test "get_run answers the Run's fields, Executions, last report with its checks, and events" do
    asked = stub_run("r1", summary("r1", state: "done", seq: 5, kind: "report", executions: 2,
                                         pr: { "url" => "https://bitbucket.org/acme/shop/pull-requests/9", "outcome" => "" }, active: true),
      [ event(1, "2026-11-15T10:00:00Z", "created", "run.base_branch" => "main"),
        event(2, "2026-11-15T10:00:05Z", "execution_started", "execution.id" => "x1", "execution.account" => "acct-1", "execution.vm" => "vm-a"),
        event(3, "2026-11-15T10:20:05Z", "execution_ended", "execution.id" => "x1", "execution.reason" => "needs a person",
              "execution.exit_code" => 0, "execution.account" => "acct-1", "execution.vm" => "vm-a"),
        event(4, "2026-11-15T10:30:00Z", "execution_started", "execution.id" => "x2", "execution.account" => "acct-2", "execution.vm" => "vm-b",
              "actor.kind" => "person", "actor.id" => "8", "actor.name" => "Bo"),
        event(5, "2026-11-15T10:40:00Z", "report", "execution.id" => "x2", "report.outcome" => "done", "report.summary" => "Fixed the total.",
              "report.checks" => [ { "command" => "bin/rails test", "exit_status" => 0, "time" => "2026-11-15T10:39:00Z", "output_tail" => "12 runs, 0 failures" },
                                   { "command" => "bun run lint", "exit_status" => 1, "output_tail" => "1 error" } ],
              "report.evidence" => [ "evidence/report.md", "evidence/cart.png" ]) ])

    got = call_tool(:get_run, user: @user, run: "r1")

    assert_not got.error?
    assert_requested asked
    run = got.json["run"]
    assert_equal [ "r1", "done", "checkout", "bitbucket.org/acme/shop", "web", "main", 2, true ],
      run.values_at("id", "state", "project", "repository", "folder", "base_branch", "executions", "active")
    assert_equal({ "url" => "https://bitbucket.org/acme/shop/pull-requests/9", "outcome" => "" }, run["pr"])

    first, second = got.json["executions"]
    assert_equal [ "x1", "2026-11-15T10:00:05.000Z", "2026-11-15T10:20:05.000Z", 1200, "needs a person", 0, "acct-1", "vm-a" ],
      first.values_at("id", "started_at", "ended_at", "duration_seconds", "reason", "exit_code", "account", "vm")
    assert_equal [ "x2", nil, nil, "acct-2", "vm-b" ], second.values_at("id", "ended_at", "duration_seconds", "account", "vm")
    assert_equal({ "kind" => "person", "id" => "8", "name" => "Bo" }, second["actor"])

    report = got.json["report"]
    assert_equal [ "done", "Fixed the total.", "x2", [ "evidence/report.md", "evidence/cart.png" ] ],
      report.values_at("outcome", "summary", "execution_id", "evidence")
    assert_equal [ [ "bin/rails test", "pass", "12 runs, 0 failures" ], [ "bun run lint", "fail", "1 error" ] ],
      report["checks"].map { |check| check.values_at("command", "result", "output_tail") }

    assert_equal [ 1, 2, 3, 4, 5 ], got.json["events"].map { |e| e["seq"] }
    assert_equal({ "run.base_branch" => "main" }, got.json["events"].first["details"])
    assert_equal "http://console.test/acme/runs/r1", got.link
  end

  test "get_run answers no report while the Run has none, and a tool error for a Run that has no events" do
    stub_run("r2", summary("r2", state: "queued", kind: "queued"), [ event(1, "2026-11-15T10:00:00Z", "created") ])
    got = call_tool(:get_run, user: @user, run: "r2")
    assert_nil got.json["report"]
    assert_equal [], got.json["executions"]

    stub_request(:get, "#{EventStore.base_url}/api/#{@agent.id}/runs/nope").to_return(status: 404, body: { error: "No such Run" }.to_json)
    got = call_tool(:get_run, user: @user, run: "nope")
    assert got.error?
    assert_match "No Run nope", got.text
    assert_match "list_runs", got.text
  end

  test "a Run id that cannot be one is a tool error with no read" do
    got = call_tool(:get_run, user: @user, run: "../x")
    assert got.error?
    assert_not_requested :get, /runs/
  end

  test "another Organization is refused and an unavailable EventStore is a tool error" do
    assert call_tool(:list_runs, user: @user, organization: "theirs").error?

    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/runs").to_return(status: 503, body: "")
    got = call_tool(:list_runs, user: @user)
    assert got.error?
    assert_match "EventStore", got.text
  end

  test "both tools only read, and no tool starts, messages or stops a Run" do
    assert McpTools::ListRuns.read_only?
    assert McpTools::GetRun.read_only?
    run_tools = McpServer::TOOLS.map(&:name_value).grep(/run/)
    assert_equal %w[get_run list_runs run_monitor run_sql], run_tools.sort
  end

  private

  def summary(id, state:, reason: "", seq: 3, kind: "execution_started", executions: 1, pr: { "url" => "", "outcome" => "" },
              active: false, closed: false)
    { "id" => id, "seq" => seq, "time" => "2026-11-15T11:00:00Z", "kind" => kind, "state" => state, "reason" => reason,
      "project" => "checkout", "repository" => "bitbucket.org/acme/shop", "folder" => "web", "trigger" => "cli",
      "title" => "Fix the cart total", "actor" => { "kind" => "person", "id" => "7", "name" => "Ana" }, "pr" => pr,
      "executions" => executions, "active" => active, "closed" => closed }
  end

  # A run event as EventStore answers it: a Log with flat Attributes.
  def event(seq, time, kind, extra = {})
    attributes = { "run.id" => "r1", "run.seq" => seq, "event.id" => "r1-#{seq}", "event.kind" => kind, "run.state" => "running",
                   "run.reason" => "", "factory.project" => "checkout", "repository" => "bitbucket.org/acme/shop", "folder" => "web",
                   "actor.kind" => "person", "actor.id" => "7", "actor.name" => "Ana" }
    { "id" => "0190f0c0-0000-7000-8000-00000000000#{seq}", "time" => time, "level" => "info", "message" => "Run r1: #{kind}",
      "service" => "bitbucket.org/acme/shop", "attributes" => attributes.merge(extra) }
  end

  def stub_runs(from:, to:, runs:, more: false, **filters)
    body = { from: from, to: to, **filters }
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/runs")
      .with(body: ->(sent) { JSON.parse(sent).compact == body.stringify_keys.merge("limit" => filters[:limit] || 100).compact })
      .to_return(status: 200, body: { runs: runs, more: more }.to_json, headers: { "Content-Type" => "application/json" })
  end

  def stub_run(id, run, events)
    stub_request(:get, "#{EventStore.base_url}/api/#{@agent.id}/runs/#{id}")
      .to_return(status: 200, body: { run: run, events: events }.to_json, headers: { "Content-Type" => "application/json" })
  end
end
