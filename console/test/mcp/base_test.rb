# frozen_string_literal: true

require "test_helper"

# The tool base every MCP tool is built on (McpTools::Base), through a probe
# tool each test gives a run.
class McpBaseTest < ActiveSupport::TestCase
  include McpTestHelper
  include MonitorTestHelper

  setup do
    @user = create(:user)
    @organization = create(:organization, name: "Acme", slug: "acme")
    @membership = create(:organization_user, organization: @organization, user: @user, role: :member)
    @project = create(:project, organization: @organization, name: "Shop", slug: "shop")
  end

  # organization

  test "organization may be left out when the user belongs to one" do
    answer = probe { text(organization.slug) }

    assert_not answer.error?
    assert_equal "acme", answer.text
  end

  test "organization names one of the user's Organizations" do
    join("beta")

    assert_equal "beta", probe(organization: "beta") { text(organization.slug) }.text
  end

  test "organization left out with several asks for one, listing them" do
    join("beta")

    answer = probe { text(organization.slug) }

    assert answer.error?
    assert_equal "Give organization, one of: acme, beta.", answer.text
  end

  test "a user with no Organization is told so" do
    answer = probe(user: create(:user)) { text(organization.slug) }

    assert answer.error?
    assert_equal "You don't belong to an Organization.", answer.text
  end

  test "another Organization's slug is not found, as one that doesn't exist" do
    theirs = create(:organization, slug: "theirs")
    create(:organization_user, organization: theirs, user: create(:user), role: :admin)

    assert_equal "No Organization theirs among yours: acme.", probe(organization: "theirs") { text(organization.slug) }.text
    assert_equal "No Organization nope among yours: acme.", probe(organization: "nope") { text(organization.slug) }.text
  end

  test "an Organization the user has left is not theirs" do
    @membership.discard!

    assert probe(organization: "acme") { text(organization.slug) }.error?
  end

  # project

  test "project names a Project in the Organization" do
    assert_equal "Shop", probe(project: "shop") { text(project.name) }.text
  end

  test "another Organization's Project is not found, naming the tool that lists them" do
    create(:project, organization: create(:organization, slug: "theirs"), slug: "secret")

    answer = probe(project: "secret") { text(project.name) }

    assert answer.error?
    assert_equal "No Project secret in acme. list_projects lists them.", answer.text
  end

  # range, from and to

  test "range is 1h back from now when none is given" do
    freeze_time do
      range = time_range_of
      assert_equal 1.hour.ago, range.from
      assert_equal Time.current, range.to
      assert_equal({ range: "1h" }, range.link_params)
    end
  end

  test "range takes a span of minutes, hours or days" do
    freeze_time do
      assert_equal 15.minutes.ago, time_range_of(range: "15m").from
      assert_equal 24.hours.ago, time_range_of(range: "24h").from
      assert_equal 7.days.ago, time_range_of(range: "7d").from
    end
  end

  test "a range that isn't a span is refused, saying what is one" do
    [ "1w", "0h", "3651d", "1 h", "h" ].each do |range|
      answer = probe(range: range) { text(time_range.span) }
      assert answer.error?, range
      assert_match "such as 15m, 1h, 24h or 7d", answer.text
    end
  end

  test "from and to are ISO 8601 times, kept as from and to in links" do
    range = time_range_of(from: "2026-10-17T09:00:00Z", to: "2026-10-17T10:30:00+01:00")

    assert_equal Time.utc(2026, 10, 17, 9), range.from
    assert_equal Time.utc(2026, 10, 17, 9, 30), range.to
    assert_equal({ from: "2026-10-17T09:00:00.000Z", to: "2026-10-17T09:30:00.000Z" }, range.link_params)
  end

  test "from and to must come together, without range, in order, as ISO 8601" do
    {
      { from: "2026-10-17T09:00:00Z" } => "Give both from and to.",
      { from: "2026-10-17T09:00:00Z", to: "2026-10-17T10:00:00Z", range: "1h" } => "Give range, or from and to, not both.",
      { from: "2026-10-17T10:00:00Z", to: "2026-10-17T09:00:00Z" } => "from must be before to.",
      { from: "yesterday", to: "2026-10-17T09:00:00Z" } => "from and to must be ISO 8601 times, such as 2026-10-17T09:00:00Z.",
    }.each do |arguments, message|
      answer = probe(**arguments) { text(time_range.span.to_s) }
      assert answer.error?, arguments.inspect
      assert_equal message, answer.text
    end
  end

  # Console links

  test "answers end with their Console link" do
    answer = probe { json({ a: 1, b: nil }, link: links.projects(organization)) }

    assert_equal '{"a":1,"b":null}', answer.body
    assert_equal({ "a" => 1, "b" => nil }, answer.json)
    assert_equal "http://console.test/acme/projects", answer.link
  end

  test "an explorer link holds the Query and range as the explorer reads its URL" do
    links = McpTools::Links.new("http://console.test/")
    query = "['shop']\n| where level == 'error'"

    url = URI(links.explorer(@project, range: McpTools::TimeRange.parse(range: "6h"), query: query, viz: "bars"))
    assert_equal "/acme/projects/shop/logs", url.path
    assert_equal({ "range" => "6h", "mode" => "query", "q" => query, "viz" => "bars" }, URI.decode_www_form(url.query).to_h)

    url = URI(links.explorer(@project, range: McpTools::TimeRange.parse(from: "2026-10-17T09:00:00Z", to: "2026-10-17T10:00:00Z"),
      sql: "SELECT 1"))
    assert_equal({ "from" => "2026-10-17T09:00:00.000Z", "to" => "2026-10-17T10:00:00.000Z", "mode" => "sql", "sql" => "SELECT 1" },
      URI.decode_www_form(url.query).to_h)
  end

  test "links go to a Project, a Dashboard, a Monitor and an Issue" do
    links = McpTools::Links.new("http://console.test")
    dashboard = create(:dashboard, organization: @organization)
    stub_monitor_query_check(@project)
    monitor = create(:monitor, project: @project)
    issue = create(:issue, project: @project)

    assert_equal "http://console.test/acme/projects/shop", links.project(@project)
    assert_equal "http://console.test/acme/dashboards/#{dashboard.id}", links.dashboard(dashboard)
    assert_equal "http://console.test/acme/dashboards/#{dashboard.id}?range=7d",
      links.dashboard(dashboard, range: McpTools::TimeRange.parse(range: "7d"))
    assert_equal "http://console.test/acme/monitors/#{monitor.id}", links.monitor(monitor)
    assert_equal "http://console.test/acme/projects/shop/issues/#{issue.number}", links.issue(issue)
    assert_equal "http://console.test/user_settings", links.user_settings
  end

  # Policies

  test "authorize! runs the user's policy and says what only an admin can do" do
    answer = probe { authorize!(organization.projects.sole, :update?, "change a Project's settings") && text("done") }
    assert answer.error?
    assert_equal "Only an Organization admin can change a Project's settings.", answer.text

    @membership.update!(role: :admin)
    assert_equal "done", probe { authorize!(organization.projects.sole, :update?, "change a Project's settings") && text("done") }.text
  end

  # Failures as tool errors

  test "failures are answered as tool errors the model can act on" do
    organization = @organization
    project = @project
    {
      -> { raise McpTools::ToolError, "Give a Query." } => "Give a Query.",
      -> { Dashboard.find(0) } => "No such Dashboard. list_dashboards lists them.",
      -> { LogMonitor.find_by!(name: "nope") } => "No such Monitor. list_monitors lists them.",
      -> { raise ActiveRecord::RecordNotFound } => "Not found.",
      -> { Dashboard.create!(organization: organization, name: "") } => "Not saved: Name can't be blank.",
      -> { Pundit.authorize(user, project, :update?) } => "Only an Organization admin can do this.",
      -> { raise EventStore::Rejected.new({ "errors" => [ { "message" => "unknown stage wher", "line" => 2, "column" => 3 } ] }) } =>
        "line 2, column 3: unknown stage wher",
      -> { raise EventStore::Rejected.new({ "error" => "SQL queries are read-only: DROP isn't allowed" }) } =>
        "SQL queries are read-only: DROP isn't allowed",
      -> { raise EventStore::SQLQueriesOff } => "SQL queries are turned off on this instance.",
      -> { raise ChartRuns::Busy } => "Console is busy running other Queries and charts. Run this again in a moment.",
      -> { raise ChartRuns::NotATable } => "This Query answers Logs, not a table. End it with summarize, count or project to chart it.",
      -> { raise EventStore::TimedOut.new({ "errors" => [ { "message" => "the run was stopped after 30 s, its timeout: try a shorter time range" } ] }) } =>
        "The run was stopped after 30 s, its timeout: try a shorter time range.",
      -> { raise EventStore::TimedOut.new(nil) } => "The run was stopped at its timeout. Try a shorter range or a narrower Query.",
      -> { raise EventStore::Unavailable, "connection refused" } => "EventStore is unavailable. Try again shortly.",
    }.each do |failure, message|
      answer = probe { instance_exec(&failure) }
      assert answer.error?, message
      assert_equal message, answer.text
    end
  end

  test "an unexpected failure is the server's internal error, not a tool error" do
    assert_raises(ZeroDivisionError) { probe { 1 / 0 } }
  end

  # Listing

  test "every tool on the server says whether it only reads or what it changes" do
    McpServer::TOOLS.each do |tool|
      annotations = tool.annotations_value
      assert annotations, "#{tool.name_value} has no annotations"
      assert_not_nil annotations.read_only_hint, tool.name_value
    end
  end

  test "a tool that needs SQL queries is listed only while they're on" do
    sql_tool = Class.new(McpTools::Base) { needs_sql_queries! }
    InstanceSetting.current.update!(sql_queries_enabled: false)

    assert McpTools::Whoami.listed?
    assert_not sql_tool.listed?

    InstanceSetting.current.update!(sql_queries_enabled: true)
    assert sql_tool.listed?
  end

  test "arguments build the input schema, with the required ones" do
    tool = Class.new(McpTools::Base) do
      takes_project
      argument :query, { type: "string" }, required: true
    end

    schema = tool.input_schema_value.to_h
    assert_equal %i[organization project query], schema[:properties].keys
    assert_equal %w[project query], schema[:required]
  end

  private

  # Runs a probe tool that takes a Project and a range, whose run is the
  # block, as user.
  def probe(user: @user, **arguments, &run)
    tool = Class.new(McpTools::Base) do
      takes_project
      takes_range
      define_method(:run) { instance_exec(&run) }
    end
    run_tool(tool, user: user, **arguments)
  end

  def time_range_of(**arguments)
    range = nil
    answer = probe(**arguments) { range = time_range and text("") }
    flunk answer.text if answer.error?
    range
  end

  def join(slug)
    create(:organization_user, organization: create(:organization, slug: slug), user: @user)
  end
end
