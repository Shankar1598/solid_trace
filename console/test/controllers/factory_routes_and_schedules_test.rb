# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The tracker routes and the schedules in the Factory section of a
# Project's settings. Console reads them from the Dispatcher's API as the
# signed-in user. An Admin adds and removes routes; each member sees the
# routes and the schedules, and pauses a schedule. There is no form to
# make a schedule.
class FactoryRoutesAndSchedulesTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include TwoFactorTestHelper

  DISPATCHER = "http://10.100.0.3:8080"
  LIST_URL = "#{DISPATCHER}/api/v1/projects"
  CONNECTIONS_URL = "#{DISPATCHER}/api/v1/connections"

  setup do
    stub_request(:get, %r{/config\z}).to_return(status: 503)
    @organization = create(:organization)
    @admin = create(:user)
    @organization.organization_users.create!(user: @admin, role: :admin)
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    @project = create(:project, organization: @organization, name: "Shop")
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
  end

  test "an Admin sees the routes, the tracker connections to choose from, and the paths that change them" do
    stub_list(settings(routes: [ route("notion-main", "Shop"), route("linear", "") ]))
    stub_schedules
    stub_connections(connection("notion-main", tracker: true), connection("bitbucket-main"),
                     connection("linear", tracker: true))
    sign_in_as(@admin)

    page = project_page
    assert_equal "/#{@organization.slug}/projects/#{@project.slug}/factory/routes",
                 page.dig("props", "factory_section", "routes_path")

    factory = factory_prop
    assert_equal [ { "connection" => "notion-main", "match" => "Shop" }, { "connection" => "linear", "match" => "" } ],
                 factory["routes"]
    assert_equal [ "notion-main", "linear" ], factory["tracker_connections"]
  end

  test "an Admin adds a route, and the Dispatcher gets the Project's settings with the routes before and the new one" do
    stub_list(settings(connection: "bitbucket-main", routes: [ route("notion-main", "Shop") ]))
    stub_request(:put, settings_url).to_return(json(settings))
    sign_in_as(@admin)

    post routes_url, params: { route: { connection: " linear ", match: " Shop " } }

    assert_redirected_to project_url(@project.slug, org_slug: @organization.slug)
    assert_equal "Route added", flash[:notice]
    assert_requested(:put, settings_url) do |request|
      assert_equal({ "repository" => "bitbucket.org/example/shop", "folder" => "apps/shop", "base_branch" => "develop",
                     "connection" => "bitbucket-main",
                     "routes" => [ { "connection" => "notion-main", "match" => "Shop" },
                                   { "connection" => "linear", "match" => "Shop" } ] },
                   JSON.parse(request.body))
    end
  end

  test "an Admin removes a route, and the other routes stay" do
    stub_list(settings(routes: [ route("notion-main", "Shop"), route("notion-main", "") ]))
    stub_request(:put, settings_url).to_return(json(settings))
    sign_in_as(@admin)

    delete routes_url, params: { route: { connection: "notion-main", match: "" } }

    assert_redirected_to project_url(@project.slug, org_slug: @organization.slug)
    assert_equal "Route removed", flash[:notice]
    assert_requested(:put, settings_url) do |request|
      assert_equal [ { "connection" => "notion-main", "match" => "Shop" } ], JSON.parse(request.body)["routes"]
    end
  end

  test "the removal of the last route sends an empty list, so the Dispatcher removes it" do
    stub_list(settings(routes: [ route("notion-main", "Shop") ]))
    stub_request(:put, settings_url).to_return(json(settings))
    sign_in_as(@admin)

    delete routes_url, params: { route: { connection: "notion-main", match: "Shop" } }

    assert_requested(:put, settings_url) { |request| assert_equal [], JSON.parse(request.body)["routes"] }
  end

  test "a pair that another Project has shows the Dispatcher's error under the routes" do
    stub_list(settings)
    stub_request(:put, settings_url).to_return(
      json({ error: "the Project other has the route (notion-main, \"Shop\"): two Projects cannot have the same pair" },
           status: 409),
    )
    sign_in_as(@admin)

    post routes_url, params: { route: { connection: "notion-main", match: "Shop" } }, headers: inertia
    follow_redirect!(headers: inertia)

    assert_equal "The Project other has the route (notion-main, \"Shop\"): two Projects cannot have the same pair",
                 response.parsed_body.dig("props", "errors", "factory_routes")
  end

  test "a pair that the Project has already shows the Dispatcher's error under the routes" do
    stub_list(settings(routes: [ route("notion-main", "Shop") ]))
    stub_request(:put, settings_url).to_return(
      json({ error: "the route (notion-main, \"Shop\") is in the list two times" }, status: 400),
    )
    sign_in_as(@admin)

    post routes_url, params: { route: { connection: "notion-main", match: "Shop" } }, headers: inertia
    follow_redirect!(headers: inertia)

    assert_equal "The route (notion-main, \"Shop\") is in the list two times",
                 response.parsed_body.dig("props", "errors", "factory_routes")
  end

  test "a route for a Project with no Factory settings is refused with no save at the Dispatcher" do
    stub_list
    put = stub_request(:put, settings_url)
    sign_in_as(@admin)

    post routes_url, params: { route: { connection: "notion-main", match: "Shop" } }, headers: inertia
    follow_redirect!(headers: inertia)

    assert_equal "Save the Factory settings before you add a route.",
                 response.parsed_body.dig("props", "errors", "factory_routes")
    assert_not_requested put
  end

  test "a member sees the routes and the schedules, no tracker connections, and a route change gets 403" do
    stub_list(settings(routes: [ route("notion-main", "Shop") ]))
    stub_schedules(schedule("sc1", "Nightly upkeep"))
    connections = stub_connections(connection("notion-main", tracker: true))
    put = stub_request(:put, settings_url)
    sign_in_as(@member)

    factory = factory_prop
    assert_equal [ { "connection" => "notion-main", "match" => "Shop" } ], factory["routes"]
    assert_equal [ "Nightly upkeep" ], factory["schedules"].map { |sc| sc["name"] }
    assert_not factory.key?("tracker_connections")
    assert_not_requested connections

    post routes_url, params: { route: { connection: "linear", match: "" } }
    assert_response :forbidden
    delete routes_url, params: { route: { connection: "notion-main", match: "Shop" } }
    assert_response :forbidden
    assert_not_requested put
  end

  test "each schedule shows its next time and its last Run with a link to the Run's page" do
    stub_list(settings)
    stub_schedules(
      schedule("sc1", "Nightly upkeep", next_at: "2026-11-16T02:00:00Z",
                                        last_run: { id: "run_7", state: "done", fired_at: "2026-11-15T02:00:00Z" }),
      schedule("sc2", "Weekly deps", paused: true, next_at: nil, last_run: nil),
    )
    sign_in_as(@member)

    nightly, weekly = factory_prop["schedules"]
    assert_equal({ "id" => "sc1", "name" => "Nightly upkeep", "cron" => "0 3 * * *", "time_zone" => "Europe/Berlin",
                   "base_branch" => "", "paused" => false, "next_at" => "2026-11-16T02:00:00Z",
                   "last_run" => { "id" => "run_7", "state" => "done", "fired_at" => "2026-11-15T02:00:00Z",
                                   "path" => "/#{@organization.slug}/runs/run_7" },
                   "pause_path" => "#{schedules_path}/sc1/pause", "continue_path" => "#{schedules_path}/sc1/continue" },
                 nightly)
    assert_equal true, weekly["paused"]
    assert_nil weekly["next_at"]
    assert_nil weekly["last_run"]
  end

  test "a schedule with an unsafe id is not shown, and the other schedules are" do
    stub_list(settings)
    stub_schedules(schedule("sc1", "Nightly upkeep"), schedule("bad/id", "Slash"), schedule("", "Empty"),
                   schedule("a b", "Space"), schedule("x" * 65, "Too long"))
    sign_in_as(@member)

    factory = factory_prop
    assert_nil factory["error"]
    assert_equal [ "sc1" ], factory["schedules"].map { |s| s["id"] }
  end

  test "a last Run with an unsafe id shows no last Run, so no Run page link is made from it" do
    stub_list(settings)
    stub_schedules(schedule("sc1", "Nightly upkeep", last_run: { id: "bad id/../x", state: "done" }),
                   schedule("sc2", "Weekly deps", last_run: { id: "../x", state: "done" }))
    sign_in_as(@member)

    factory = factory_prop
    assert_nil factory["error"]
    assert_equal [ "sc1", "sc2" ], factory["schedules"].map { |s| s["id"] }
    factory["schedules"].each { |s| assert_nil s["last_run"] }
  end

  test "a member pauses a schedule at once, and continues it" do
    stub_request(:post, "#{DISPATCHER}/api/v1/schedules/sc1/pause")
      .to_return(json(schedule("sc1", "Nightly upkeep", paused: true, next_at: nil)))
    continue = stub_request(:post, "#{DISPATCHER}/api/v1/schedules/sc1/continue")
      .to_return(json(schedule("sc1", "Nightly upkeep")))
    sign_in_as(@member)

    post "#{schedules_path}/sc1/pause"
    assert_redirected_to project_url(@project.slug, org_slug: @organization.slug)
    assert_equal "Schedule Nightly upkeep paused", flash[:notice]
    assert_requested(:post, "#{DISPATCHER}/api/v1/schedules/sc1/pause") do |request|
      assert_match(/\ABearer stc_/, request.headers["Authorization"])
    end

    post "#{schedules_path}/sc1/continue"
    assert_equal "Schedule Nightly upkeep continues", flash[:notice]
    assert_requested continue
  end

  test "a pause that the Dispatcher refuses shows its error" do
    stub_request(:post, "#{DISPATCHER}/api/v1/schedules/sc9/pause").to_return(json({ error: "no schedule sc9" },
                                                                                   status: 404))
    sign_in_as(@member)

    post "#{schedules_path}/sc9/pause"

    assert_equal "The Dispatcher did not pause the schedule: no schedule sc9", flash[:alert]
  end

  test "there is no form to make a schedule" do
    calls = stub_request(:any, /:8080/)
    sign_in_as(@admin)

    post schedules_path, params: { schedule: { name: "Nightly", cron: "0 3 * * *", prompt: "Upkeep" } }

    assert_response :not_found
    assert_not_requested calls
  end

  test "with no Dispatcher URL, the route and pause paths give 404 and call no Dispatcher" do
    InstanceSetting.current.update!(dispatcher_url: nil)
    calls = stub_request(:any, /:8080/)
    sign_in_as(@admin)

    post routes_url, params: { route: { connection: "notion-main", match: "Shop" } }
    assert_response :not_found
    post "#{schedules_path}/sc1/pause"
    assert_response :not_found
    assert_not_requested calls
  end

  test "a person of another Organization gets 404 for a pause" do
    outsider = create(:user)
    create(:organization).organization_users.create!(user: outsider, role: :admin)
    calls = stub_request(:any, /:8080/)
    sign_in_as(outsider)

    post "#{schedules_path}/sc1/pause"

    assert_response :not_found
    assert_not_requested calls
  end

  private

  def routes_url
    "/#{@organization.slug}/projects/#{@project.slug}/factory/routes"
  end

  def schedules_path
    "/#{@organization.slug}/projects/#{@project.slug}/factory/schedules"
  end

  def settings_url
    "#{DISPATCHER}/api/v1/projects/#{@project.slug}/settings"
  end

  def settings(connection: "", routes: [])
    { project: @project.slug, repository: "bitbucket.org/example/shop", folder: "apps/shop", base_branch: "develop",
      connection: connection, routes: routes, orphaned: false }
  end

  def route(connection, match)
    { connection: connection, match: match }
  end

  def connection(name, tracker: false)
    { name: name, kind: tracker ? "notion" : "bitbucket", webhook_secret: false }
      .merge(tracker ? { tracker: { ready_status: "Ready for agent", routing_field: "Project" } } : {})
  end

  def schedule(id, name, paused: false, next_at: "2026-11-16T02:00:00Z", last_run: nil)
    { id: id, project: @project.slug, name: name, cron: "0 3 * * *", time_zone: "Europe/Berlin",
      prompt: "Update the dependencies.", base_branch: "", paused: paused, next_at: next_at, last_run: last_run,
      created_by: { id: "u1", name: "Ada" } }
  end

  def stub_list(*projects)
    stub_request(:get, LIST_URL).to_return(json({ projects: projects }))
  end

  def stub_schedules(*schedules)
    stub_request(:get, "#{DISPATCHER}/api/v1/schedules?project=#{@project.slug}")
      .to_return(json({ schedules: schedules }))
  end

  def stub_connections(*connections)
    stub_request(:get, CONNECTIONS_URL).to_return(json({ connections: connections }))
  end

  def json(body, status: 200)
    { status: status, headers: { "Content-Type" => "application/json" }, body: body.to_json }
  end

  def project_page
    get project_url(@project.slug, org_slug: @organization.slug), headers: inertia
    assert_response :success
    response.parsed_body
  end

  # The deferred factory prop, as the page loads it after it shows.
  def factory_prop
    get project_url(@project.slug, org_slug: @organization.slug),
      headers: inertia.merge("X-Inertia-Partial-Component" => "Projects/Show", "X-Inertia-Partial-Data" => "factory")
    assert_response :success
    response.parsed_body.dig("props", "factory")
  end
end
