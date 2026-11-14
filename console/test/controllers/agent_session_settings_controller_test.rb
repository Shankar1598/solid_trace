# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# Organization settings → Agent sessions: an Admin turns on Agent sessions,
# which makes the Organization's Agent Project, and edits its captured
# repositories; members read them. A turn-on and each added entry need the
# confirm checkbox, and each confirm is recorded.
class AgentSessionSettingsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include McpTestHelper

  setup do
    @organization = create(:organization)
    @admin = create(:user, name: "Ada")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user, name: "Bob")
    create(:organization_user, organization: @organization, user: @member, role: :member)
  end

  test "an Admin turns on Agent sessions with a captured repository and the confirm, which makes the Agent Project" do
    sign_in_as(@admin)

    turn_on([ "github.com/acme/shop" ], confirmed: "1")

    assert_redirected_to agent_session_settings_path(org_slug: @organization.slug)
    page = settings_page
    assert page["agent_sessions"]["on"]
    assert_equal [ "github.com/acme/shop" ], page["agent_sessions"]["captured_repositories"]
    assert_equal 180, page["agent_sessions"]["project"]["log_retention_days"]
    assert_equal 1, @organization.projects.count
  end

  test "a turn-on with no captured repository, or without the confirm, is refused and makes no Project" do
    sign_in_as(@admin)

    turn_on([ "" ], confirmed: "1")
    assert_equal [ "need at least one repository to turn on Agent sessions" ], inertia_errors["captured_repositories"]

    turn_on([ "github.com/acme/shop" ])
    assert inertia_errors["confirmed"].present?

    turn_on([ "github.com/acme/shop" ], confirmed: "0")
    assert inertia_errors["confirmed"].present?

    assert_not settings_page["agent_sessions"]["on"]
    assert_equal 0, @organization.projects.count
  end

  test "a second turn-on makes no second Agent Project and keeps the list" do
    sign_in_as(@admin)
    turn_on([ "github.com/acme/shop" ], confirmed: "1")

    turn_on([ "github.com/acme/other" ], confirmed: "1")

    assert_equal [ "Agent sessions are already on." ], inertia_errors["base"]
    assert_equal 1, @organization.projects.count
    assert_equal [ "github.com/acme/shop" ], settings_page["agent_sessions"]["captured_repositories"]
  end

  test "the Agent Project takes a free name when a Project has its slug" do
    create(:project, organization: @organization, name: "Agent sessions")
    sign_in_as(@admin)

    turn_on([ "github.com/acme/shop" ], confirmed: "1")

    assert_equal "Agent sessions 2", settings_page["agent_sessions"]["project"]["name"]
  end

  test "an entry is host/workspace/name or host/workspace/*, and other forms are refused at save" do
    sign_in_as(@admin)
    turn_on([ "GitHub.com/acme/shop.git", " gitlab.example.com/platform/* " ], confirmed: "1")
    assert_equal [ "github.com/acme/shop", "gitlab.example.com/platform/*" ], settings_page["agent_sessions"]["captured_repositories"]

    [ "github.com/acme", "github.com/acme/shop/api", "https://github.com/acme/shop", "git@github.com:acme/shop",
      "github.com/*/*", "github.com/*", "*/acme/shop", "github.com/acme/sh op", "github.com/../shop", "github.com/acme/sh*" ].each do |entry|
      save_list([ "github.com/acme/shop", entry ], confirmed: "1")

      assert inertia_errors["captured_repositories"].present?, "#{entry.inspect} should be refused"
    end
    assert_equal [ "github.com/acme/shop", "gitlab.example.com/platform/*" ], settings_page["agent_sessions"]["captured_repositories"]
  end

  test "an added entry needs the confirm, a removed one doesn't, and each confirm records who, when and the entries" do
    sign_in_as(@admin)
    travel_to(Time.utc(2026, 11, 14, 9)) { turn_on([ "github.com/acme/shop" ], confirmed: "1") }

    save_list([ "github.com/acme/shop", "github.com/acme/api" ])
    assert inertia_errors["confirmed"].present?
    assert_equal [ "github.com/acme/shop" ], settings_page["agent_sessions"]["captured_repositories"]

    travel_to(Time.utc(2026, 11, 14, 10)) { save_list([ "github.com/acme/shop", "github.com/acme/api" ], confirmed: "1") }
    assert_redirected_to agent_session_settings_path(org_slug: @organization.slug)

    save_list([ "github.com/acme/api" ])
    assert_redirected_to agent_session_settings_path(org_slug: @organization.slug)
    save_list([])
    assert_redirected_to agent_session_settings_path(org_slug: @organization.slug)

    page = settings_page["agent_sessions"]
    assert_equal [], page["captured_repositories"]
    assert_equal [
      { "user" => "Ada", "at" => "2026-11-14T10:00:00Z", "entries" => [ "github.com/acme/api" ] },
      { "user" => "Ada", "at" => "2026-11-14T09:00:00Z", "entries" => [ "github.com/acme/shop" ] }
    ], page["confirmations"]
  end

  test "a member reads the list, and each write of theirs is 403" do
    agent_project = turn_on_as_admin
    sign_in_as(@member)

    page = settings_page
    assert_equal [ "github.com/acme/shop" ], page["agent_sessions"]["captured_repositories"]
    assert_equal false, page["can_manage"]

    save_list([ "github.com/acme/shop", "github.com/acme/api" ], confirmed: "1")
    assert_response :forbidden
    turn_on([ "github.com/acme/api" ], confirmed: "1")
    assert_response :forbidden
    assert_equal [ "github.com/acme/shop" ], agent_project.reload.captured_repositories
  end

  test "someone outside the Organization gets 404" do
    turn_on_as_admin
    outsider = create(:user)
    sign_in_as(outsider)

    get agent_session_settings_path(org_slug: @organization.slug)
    assert_response :not_found
    save_list([ "github.com/acme/api" ], confirmed: "1")
    assert_response :not_found
  end

  test "an Admin changes the Agent Project's Retention as for any Project" do
    agent_project = turn_on_as_admin
    sign_in_as(@admin)

    patch project_path(agent_project, org_slug: @organization.slug), params: { project: { log_retention_days: "90" } }

    assert_redirected_to project_path(agent_project, org_slug: @organization.slug)
    assert_equal 90, settings_page["agent_sessions"]["project"]["log_retention_days"]
  end

  test "a member runs a Query on the Agent Project's Logs, as for any Project" do
    agent_project = turn_on_as_admin
    query = stub_request(:post, "#{EventStore.base_url}/api/#{agent_project.id}/logs/query")
      .to_return(status: 200, body: { logs: [ { message: "a transcript line" } ] }.to_json)
    sign_in_as(@member)

    get query_project_logs_url(agent_project, org_slug: @organization.slug),
      params: { query: "", from: "2026-11-14T09:00:00Z", to: "2026-11-14T10:00:00Z" }

    assert_response :success
    assert_requested query
    assert_equal "a transcript line", response.parsed_body["logs"].first["message"]
  end

  test "the Agent Project gets no Log transform: a save of one is refused" do
    agent_project = turn_on_as_admin
    check = stub_request(:post, "#{EventStore.base_url}/api/#{agent_project.id}/logs/transform/check")
    stub_request(:get, "#{EventStore.base_url}/api/#{agent_project.id}/logs/transform/stats").to_return(status: 503)
    sign_in_as(@admin)

    patch project_log_transform_url(agent_project, org_slug: @organization.slug), params: { program: ".a = 1" },
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }

    assert_response :success
    page = JSON.parse(response.body)["props"]
    assert_equal [ LogTransformSave::AGENT_PROJECT ], page["errors"]["program"]
    assert_equal false, page["can_manage"]
    assert_not_requested check
    assert_nil agent_project.log_transform
  end

  test "the Agent Project gets no Log transform over MCP either" do
    agent_project = turn_on_as_admin

    answer = call_tool(:update_log_transform, user: @admin, organization: @organization.slug, project: agent_project.slug, program: ".a = 1")

    assert answer.error?
    assert_includes answer.text, LogTransformSave::AGENT_PROJECT
    assert_nil agent_project.log_transform
  end

  private

  def turn_on_as_admin
    AgentSessionsSettings.turn_on(organization: @organization, user: @admin, entries: [ "github.com/acme/shop" ], confirmed: true).project
  end

  def inertia_errors
    follow_redirect!(headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest })
    JSON.parse(response.body)["props"]["errors"] || {}
  end

  def turn_on(entries, confirmed: nil)
    post agent_session_settings_path(org_slug: @organization.slug),
      params: { captured_repositories: entries, confirmed: confirmed }.compact
  end

  def save_list(entries, confirmed: nil)
    patch agent_session_settings_path(org_slug: @organization.slug),
      params: { captured_repositories: entries.presence || [ "" ], confirmed: confirmed }.compact
  end

  def settings_page
    get agent_session_settings_path(org_slug: @organization.slug),
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "Settings/AgentSessions", JSON.parse(response.body)["component"]
    JSON.parse(response.body)["props"]
  end
end
