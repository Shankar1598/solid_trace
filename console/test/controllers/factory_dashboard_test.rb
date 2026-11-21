# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The starter 'Factory' Dashboard: made one time for an Organization, on the
# Agent Project, when Console first sees the Organization's Factory settings
# (a save in the Factory section, a read of the section, or the first read
# of the Runs page that finds a Run), with a Dashboard filter for the Project
# and four Charts from the closed run events.
class FactoryDashboardTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include TwoFactorTestHelper

  DISPATCHER = "http://10.100.0.3:8080"
  LIST_URL = "#{DISPATCHER}/api/v1/projects"

  setup do
    stub_request(:get, %r{/config\z}).to_return(status: 503)
    @organization = create(:organization, slug: "acme")
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @shop = create(:project, organization: @organization, name: "Shop", slug: "shop")
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    @agent = AgentSessionsSettings.turn_on(organization: @organization, user: @admin, entries: [ "github.com/acme/shop" ],
                                           confirmed: "1").project
  end

  test "the first save of Factory settings makes the Dashboard on the Agent Project, and a later save makes no second one" do
    stub_dispatcher(settings: nil)
    sign_in_as(@admin)

    save_settings
    assert_equal 1, factory_dashboards.size
    dashboard = factory_dashboards.first
    assert_equal @admin, dashboard.created_by
    assert_equal "30d", dashboard.default_range

    save_settings("bitbucket.org/example/other")
    assert_equal [ dashboard.id ], factory_dashboards.map(&:id)
    assert_equal 4, dashboard.charts.count
  end

  test "a save that the Dispatcher refuses makes no Dashboard" do
    stub_dispatcher(settings: nil)
    stub_request(:put, settings_url).to_return(status: 422, body: { error: "repository is not valid" }.to_json,
                                                headers: { "Content-Type" => "application/json" })
    sign_in_as(@admin)

    patch project_factory_url(@shop.slug, org_slug: "acme"), params: { factory: { repository: "x" } }

    assert_empty factory_dashboards
  end

  test "the Dashboard has a Project filter and four Charts on the Agent Project, laid out two by two" do
    stub_dispatcher(settings: nil)
    sign_in_as(@admin)
    save_settings

    dashboard = factory_dashboards.first
    assert_equal [ { "name" => "project", "label" => "Project", "kind" => "select", "multiple" => true,
                     "options" => { "from" => "field", "project_id" => @agent.id, "field" => "factory.project" } } ],
      dashboard.filters
    charts = dashboard.charts
    assert_equal [ "Tokens for each merged PR", "Cycle time of merged PRs", "Acceptance rate", "Interventions for each Run" ],
      charts.map(&:title)
    assert_equal [ @agent.id ], charts.map(&:project_id).uniq
    assert_equal [ [ 0, 0 ], [ 6, 0 ], [ 0, 4 ], [ 6, 4 ] ], charts.map { |chart| [ chart.x, chart.y ] }
    # Each reads the closing events, and the Project filter.
    charts.each do |chart|
      assert_includes chart.query, 'event.kind == "closed"'
      assert_includes chart.query, "factory.project == $project"
      assert_includes chart.query, "by bin_auto(time)"
    end
  end

  test "the tokens, cycle time and interventions Charts read the totals of the closing event" do
    stub_dispatcher(settings: nil)
    sign_in_as(@admin)
    save_settings

    tokens, cycle, _acceptance, interventions = factory_dashboards.first.charts.map(&:query)
    %w[input output cache_read cache_write].each { |kind| assert_includes tokens, "totals.tokens.#{kind}" }
    assert_includes tokens, 'run.close_reason == "pr_merged"'
    assert_includes cycle, "totals.seconds_to_pr_opened"
    assert_includes cycle, "totals.seconds_from_pr_opened_to_outcome"
    assert_includes cycle, 'run.close_reason == "pr_merged"'
    assert_includes interventions, "totals.interventions.total"
  end

  test "the acceptance rate divides two countif counts: merged over merged and declined" do
    stub_dispatcher(settings: nil)
    sign_in_as(@admin)
    save_settings

    acceptance = factory_dashboards.first.charts.find_by!(title: "Acceptance rate").query
    merged = 'countif(run.close_reason == "pr_merged")'
    declined = 'countif(run.close_reason == "pr_declined")'
    assert_includes acceptance, "100 * #{merged} / (#{merged} + #{declined})"
    assert_equal 3, acceptance.scan("countif(").size
  end

  test "the Project filter changes each of the four Charts: its values go to EventStore as the project parameter" do
    stub_dispatcher(settings: nil)
    sign_in_as(@admin)
    save_settings
    dashboard = factory_dashboards.first
    table = { columns: [ { name: "time", type: "datetime", bin_seconds: 43_200 }, { name: "n", type: "number", agg: "avg" } ],
              rows: [ [ "2026-10-03T00:00:00Z", 5 ] ], truncated: false }
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/logs/query")
      .to_return(status: 200, body: { table: table }.to_json)
    range = "from=2026-10-01T00:00:00Z&to=2026-10-03T10:00:00Z"

    dashboard.charts.each do |chart|
      get data_dashboard_chart_url(dashboard, chart, org_slug: "acme") + "?#{range}&v_project=shop&v_project=api"
      assert_response :success
      assert_requested(:post, "#{EventStore.base_url}/api/#{@agent.id}/logs/query") do |request|
        body = JSON.parse(request.body)
        body["query"] == chart.query && body["parameters"] == { "project" => [ "shop", "api" ] }
      end
    end
  end

  test "settings that the CLI saved make the Dashboard at the next read of the Factory section, one time" do
    stub_dispatcher(settings: settings_row)
    sign_in_as(@member)

    assert_empty factory_dashboards
    read_section
    read_section

    assert_equal 1, factory_dashboards.size
    assert_equal @member, factory_dashboards.first.created_by
  end

  test "a read of the Factory section of a Project with no settings makes no Dashboard" do
    stub_dispatcher(settings: nil)
    sign_in_as(@member)

    read_section

    assert_empty factory_dashboards
  end

  test "the first read of the Runs page that finds a Run makes the Dashboard, one time" do
    sign_in_as(@member)
    stub_runs([])
    runs_page
    assert_empty factory_dashboards

    stub_runs([ run_row ])
    runs_page
    runs_page

    assert_equal 1, factory_dashboards.size
  end

  test "a member who deletes the Dashboard does not get a new one at the next read" do
    stub_dispatcher(settings: settings_row)
    sign_in_as(@admin)
    read_section
    factory_dashboards.destroy_all

    read_section
    save_settings
    stub_runs([ run_row ])
    runs_page

    assert_empty factory_dashboards
  end

  test "an Organization with no Agent Project gets the Dashboard when it has one" do
    stub_dispatcher(settings: settings_row)
    other = create(:organization, slug: "other")
    admin = create(:user)
    create(:organization_user, organization: other, user: admin, role: :admin)
    project = create(:project, organization: other, slug: "shop")
    stub_request(:get, "#{DISPATCHER}/api/v1/schedules?project=shop").to_return(json({ schedules: [] }))
    sign_in_as(admin)

    get project_url(project.slug, org_slug: "other"), headers: partial_headers
    assert_empty other.dashboards

    AgentSessionsSettings.turn_on(organization: other, user: admin, entries: [ "github.com/other/shop" ], confirmed: "1")
    get project_url(project.slug, org_slug: "other"), headers: partial_headers

    assert_equal [ "Factory" ], other.dashboards.reload.map(&:name)
  end

  test "a Dashboard that cannot be made does not fail the page and is tried again at the next read" do
    stub_dispatcher(settings: settings_row)
    sign_in_as(@member)
    broken = ->(*) { raise ActiveRecord::RecordInvalid }
    Dashboard.stub(:transaction, broken) { read_section }
    assert_response :success
    assert_nil @organization.reload.factory_dashboard_made_at

    read_section

    assert_equal 1, factory_dashboards.size
  end

  private

  def factory_dashboards
    @organization.dashboards.where(name: "Factory")
  end

  def settings_url
    "#{DISPATCHER}/api/v1/projects/shop/settings"
  end

  def settings_row(repository: "bitbucket.org/example/shop")
    { project: "shop", repository: repository, folder: "", base_branch: "", connection: "", orphaned: false }
  end

  def stub_dispatcher(settings:)
    stub_request(:get, "#{DISPATCHER}/api/v1/schedules?project=shop").to_return(json({ schedules: [] }))
    stub_request(:get, "#{DISPATCHER}/api/v1/connections").to_return(json({ connections: [] }))
    stub_request(:get, LIST_URL).to_return(json({ projects: [ settings ].compact }))
    stub_request(:put, settings_url).to_return(json(settings_row))
  end

  def save_settings(repository = "bitbucket.org/example/shop")
    patch project_factory_url(@shop.slug, org_slug: "acme"), params: { factory: { repository: repository } }
    assert_redirected_to project_url(@shop.slug, org_slug: "acme")
  end

  def partial_headers
    inertia.merge("X-Inertia-Partial-Component" => "Projects/Show", "X-Inertia-Partial-Data" => "factory")
  end

  def read_section
    get project_url(@shop.slug, org_slug: "acme"), headers: partial_headers
    assert_response :success
  end

  def run_row
    { id: "run_1", seq: 3, time: "2026-10-03T10:00:00Z", kind: "report", state: "done", reason: "", project: "shop",
      repository: "bitbucket.org/acme/shop", folder: "", trigger: "cli", title: "Fix the cart total",
      actor: { kind: "person", id: "7", name: "Ana" }, pr: { url: "", outcome: "" }, executions: 1,
      active: false, closed: false }
  end

  def stub_runs(runs)
    WebMock.reset!
    stub_request(:get, %r{/config\z}).to_return(status: 503)
    stub_request(:post, "#{EventStore.base_url}/api/#{@agent.id}/runs")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { runs: runs, more: false }.to_json)
  end

  def runs_page
    travel_to Time.utc(2026, 10, 3, 12, 0, 0) do
      get runs_path(org_slug: "acme"), headers: inertia
    end
    assert_response :success
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end

  def json(body, status: 200)
    { status: status, headers: { "Content-Type" => "application/json" }, body: body.to_json }
  end
end
