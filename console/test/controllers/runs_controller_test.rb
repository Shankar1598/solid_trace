# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Runs page (Organization navigation): the Runs of the Organization's
# Agent Project from their run events, as EventStore's /runs answers them,
# Active Runs first, over 30 days by default, with the Project and state
# filters in the URL. Each member sees it; someone outside the Organization
# gets 404.
class RunsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

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

  def run_row(id, state:, active:, reason: "", time: "2026-10-03T10:00:00Z")
    {
      id: id, seq: 3, time: time, kind: "report", state: state, reason: reason, project: "shop",
      repository: "bitbucket.org/acme/shop", folder: "web", trigger: "cli", title: "Fix the cart total",
      actor: { kind: "person", id: "7", name: "Ana" }, pr: { url: "", outcome: "" }, executions: 1,
      active: active, closed: false,
    }
  end

  def stub_runs(project, body, runs: [ run_row("run_waiting", state: "waiting_on_person", active: true, reason: "needs a person"),
                                       run_row("run_done", state: "done", active: false, time: "2026-10-02T10:00:00Z") ], more: false)
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/runs")
      .with(body: hash_including(body))
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { runs: runs, more: more }.to_json)
  end

  def page(params = {})
    get runs_path(org_slug: "acme", **params), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "Runs/Index", response.parsed_body["component"]
    response.parsed_body.dig("props", "runs")
  end

  test "a member sees the Runs of the last 30 days in the order EventStore gives, Active Runs first" do
    project = turn_on
    asked = stub_runs(project, { "from" => "2026-09-03T12:00:00.000Z", "to" => "2026-10-03T12:00:00.000Z" })
    sign_in_as(@member)

    view = page

    assert_requested asked
    assert view["on"]
    assert_equal "30d", view.dig("range", "span")
    waiting, done = view["rows"]
    assert_equal [ "run_waiting", "run_done" ], [ waiting["id"], done["id"] ]
    assert_equal [ "waiting_on_person", "needs a person", "shop", "bitbucket.org/acme/shop", "cli", "Fix the cart total" ],
      waiting.values_at("state", "reason", "project", "repository", "trigger", "title")
    assert_equal "Ana", waiting.dig("actor", "name")
    assert waiting["active"]
    assert_not done["active"]
    assert_equal({ "project" => nil, "state" => nil }, view["filters"])
    # The Projects whose Runs can be listed: not the Agent Project itself.
    assert_equal [ { "name" => "Shop", "slug" => "shop" } ], view["projects"]
    assert_equal false, view["more"]
    assert view.dig("event_store", "available")
  end

  test "when EventStore cut the list at its limit, the page says that there are more Runs" do
    project = turn_on
    stub_runs(project, {}, more: true)
    sign_in_as(@member)

    assert_equal true, page["more"]
  end

  test "each state of a Run (GLOSSARY, Run) is a choice of the state filter and goes to EventStore" do
    project = turn_on
    states = %w[queued running waiting_on_person waiting_on_account_limit taken_over done failed stopped]
    sign_in_as(@member)

    states.each do |state|
      asked = stub_runs(project, { "state" => state })

      view = page(state: state)

      assert_requested asked
      assert_equal state, view.dig("filters", "state")
      assert_equal states, view["states"]
    end
  end

  test "the Project filter is trimmed, and a blank one is not sent" do
    project = turn_on
    blank = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/runs")
      .with { |request| !JSON.parse(request.body).key?("project") }
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { runs: [], more: false }.to_json)
    sign_in_as(@member)

    assert_nil page(project: "  ").dig("filters", "project")
    assert_requested blank

    trimmed = stub_runs(project, { "project" => "shop" })

    assert_equal "shop", page(project: "shop ").dig("filters", "project")
    assert_requested trimmed
  end

  test "the Project and state filters in the URL go to EventStore and come back on the page" do
    project = turn_on
    asked = stub_runs(project, { "project" => "shop", "state" => "waiting_on_person" })
    sign_in_as(@member)

    view = page(project: "shop", state: "waiting_on_person")

    assert_requested asked
    assert_equal({ "project" => "shop", "state" => "waiting_on_person" }, view["filters"])
  end

  test "a longer range is the member's choice" do
    project = turn_on
    asked = stub_runs(project, { "from" => "2026-07-05T12:00:00.000Z", "to" => "2026-10-03T12:00:00.000Z" })
    sign_in_as(@member)

    view = page(range: "90d")

    assert_requested asked
    assert_equal "90d", view.dig("range", "span")
  end

  test "a state that is not a Run's state is not sent, and a range that cannot be read is the default" do
    project = turn_on
    asked = stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/runs")
      .with { |request| (JSON.parse(request.body).keys - %w[from to project]).empty? && JSON.parse(request.body)["from"] == "2026-09-03T12:00:00.000Z" }
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { runs: [], more: false }.to_json)
    sign_in_as(@member)

    view = page(range: "forever", state: "sleeping")

    assert_requested asked
    assert_equal({ "project" => nil, "state" => nil }, view["filters"])
    assert_equal "30d", view.dig("range", "span")
    assert_equal [], view["rows"]
  end

  test "when EventStore cannot answer, the page says why and has no rows" do
    project = turn_on
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/runs").to_return(status: 500, body: { error: "Internal error" }.to_json)
    sign_in_as(@member)

    view = page

    assert_not view.dig("event_store", "available")
    assert_match "EventStore can't read the Runs", view.dig("event_store", "error")
    assert_equal [], view["rows"]
  end

  test "with no Agent Project, the page says that Runs are recorded there and reads nothing" do
    sign_in_as(@member)

    view = page

    assert_equal false, view["on"]
    assert_not_requested :post, %r{/runs}
  end

  test "a person outside the Organization gets 404" do
    turn_on
    sign_in_as(create(:user))

    get runs_path(org_slug: "acme")

    assert_response :not_found
  end

  test "each Organization with an Agent Project reads the Runs of its own, never another Organization's" do
    acme_project = turn_on
    other = create(:organization, slug: "other")
    other_admin = create(:user)
    create(:organization_user, organization: other, user: other_admin, role: :admin)
    other_project = AgentSessionsSettings.turn_on(organization: other, user: other_admin, entries: [ "github.com/other/app" ], confirmed: "1").project
    acme_asked = stub_runs(acme_project, {})
    other_asked = stub_runs(other_project, {}, runs: [ run_row("run_other", state: "running", active: true) ])
    sign_in_as(other_admin)

    get runs_path(org_slug: "other"), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }

    assert_response :success
    assert_equal [ "run_other" ], response.parsed_body.dig("props", "runs", "rows").map { |row| row["id"] }
    assert_requested other_asked
    assert_not_requested acme_asked
  end
end
