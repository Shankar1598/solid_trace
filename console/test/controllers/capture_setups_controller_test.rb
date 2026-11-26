# frozen_string_literal: true

require "test_helper"

# Seam 2: the capture setup page (Organization settings → Agent sessions →
# Capture setup). Each member reads it: the install command of the
# instance's version, the plugin, the managed-settings snippet, the login
# command with the Ingest URL, and the captured repositories.
class CaptureSetupsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  RELEASE = "https://github.com/Shankar1598/solid_trace/releases/download/v0.19.0"

  setup do
    @organization = create(:organization, name: "Acme")
    @admin = create(:user)
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    InstanceSetting.current.update!(console_url: "https://solidtrace.example.com", ingest_url: "https://solidtrace.example.com:4443")
    @version = Rails.application.config.x.solidtrace_version
    Rails.application.config.x.solidtrace_version = "v0.19.0"
  end

  teardown do
    Rails.application.config.x.solidtrace_version = @version
  end

  test "a member reads the install command of the instance's version, the login command and the list" do
    turn_on([ "github.com/acme/shop", "github.com/acme/*" ])
    sign_in_as(@member)

    page = setup_page

    assert_equal "v0.19.0", page["version"]
    assert_equal "curl -fsSL #{RELEASE}/install.sh | sh", page["install_command"]
    assert_equal "solidtrace login --ingest-url https://solidtrace.example.com:4443 https://solidtrace.example.com",
                 page["login_command"]
    assert_equal "solidtrace capture status", page["status_command"]
    assert_equal [ "github.com/acme/shop", "github.com/acme/*" ], page["captured_repositories"]
    assert page["on"]
  end

  # The marketplace file is in the repository: Claude Code clones only
  # .claude-plugin and the plugin's folder, at the release's tag.
  test "the plugin install commands add the repository's marketplace at the release's tag and install solidtrace@solidtrace" do
    sign_in_as(@member)

    page = setup_page

    assert_equal [ "claude plugin marketplace add Shankar1598/solid_trace#v0.19.0 --sparse .claude-plugin cli/plugin",
                   "claude plugin install solidtrace@solidtrace" ],
                 page["plugin_commands"]
    assert_equal "claude plugin list", page["plugin_check_command"]
  end

  test "the managed-settings snippet registers the repository's marketplace at the release's tag and force-enables the plugin" do
    sign_in_as(@admin)

    snippet = JSON.parse(setup_page["managed_settings"])

    assert_equal({ "extraKnownMarketplaces" => { "solidtrace" => { "source" => {
                     "source" => "github", "repo" => "Shankar1598/solid_trace", "ref" => "v0.19.0",
                     "sparsePaths" => [ ".claude-plugin", "cli/plugin" ] } } },
                   "enabledPlugins" => { "solidtrace@solidtrace" => true } }, snippet)
  end

  test "an instance that runs no release gets the newest release's CLI and the marketplace of main" do
    Rails.application.config.x.solidtrace_version = "dev"
    sign_in_as(@member)

    page = setup_page

    latest = "https://github.com/Shankar1598/solid_trace/releases/latest/download"
    assert_equal "curl -fsSL #{latest}/install.sh | sh", page["install_command"]
    assert_equal "claude plugin marketplace add Shankar1598/solid_trace --sparse .claude-plugin cli/plugin", page["plugin_commands"].first
    source = JSON.parse(page["managed_settings"]).dig("extraKnownMarketplaces", "solidtrace", "source")
    assert_equal "Shankar1598/solid_trace", source["repo"]
    assert_not source.key?("ref")
    assert page["version_note"].present?
  end

  test "with an http:// Ingest URL and an https:// Console, the login command and step 5 use ingest's HTTPS address" do
    InstanceSetting.current.update!(console_url: "https://solidtrace.example.com", ingest_url: "http://solidtrace.example.com:4000")
    create(:project_key, project: turn_on([ "github.com/acme/shop" ]).project)
    sign_in_as(@member)

    page = setup_page
    assert_equal "solidtrace login --ingest-url https://solidtrace.example.com:4443 https://solidtrace.example.com", page["login_command"]
    assert_includes page["telemetry"].to_json, "https://solidtrace.example.com:4443"
  end

  test "with no Console URL saved or set, the login command uses the address the request reached Console at" do
    InstanceSetting.current.update!(console_url: nil, ingest_url: nil)
    sign_in_as(@member)

    assert_equal "solidtrace login --ingest-url http://www.example.com:4000 http://www.example.com",
                 with_env("CONSOLE_URL" => nil, "PUBLIC_INGEST_URL" => nil) { setup_page["login_command"] }
  end

  test "step 5 sends Claude Code's telemetry to the Ingest URL with the Agent Project's key, which each member reads" do
    key = create(:project_key, project: turn_on([ "github.com/acme/shop" ]).project)
    sign_in_as(@member)

    telemetry = setup_page["telemetry"]

    env = { "CLAUDE_CODE_ENABLE_TELEMETRY" => "1",
            "OTEL_LOGS_EXPORTER" => "otlp",
            "OTEL_METRICS_EXPORTER" => "otlp",
            "OTEL_EXPORTER_OTLP_PROTOCOL" => "http/protobuf",
            "OTEL_EXPORTER_OTLP_ENDPOINT" => "https://solidtrace.example.com:4443",
            "OTEL_EXPORTER_OTLP_HEADERS" => "Authorization=Bearer #{key.public_key}",
            "OTEL_METRICS_INCLUDE_REPOSITORY" => "1" }
    assert_equal({ "env" => env }, JSON.parse(telemetry["user_settings"]))
    assert_equal env, JSON.parse(telemetry["managed_settings"])["env"]
    assert telemetry["has_key"]
  end

  test "step 5's managed settings also push the plugin, as step 2's do, so an Admin pastes one block" do
    create(:project_key, project: turn_on([ "github.com/acme/shop" ]).project)
    sign_in_as(@admin)

    page = setup_page

    managed = JSON.parse(page["telemetry"]["managed_settings"])
    assert_equal JSON.parse(page["managed_settings"]), managed.except("env")
  end

  test "with no key yet, step 5 shows <key> and links to the Agent Project's page, where an Admin makes it" do
    project = turn_on([ "github.com/acme/shop" ]).project
    sign_in_as(@member)

    telemetry = setup_page["telemetry"]

    assert_equal "Authorization=Bearer <key>", JSON.parse(telemetry["user_settings"]).dig("env", "OTEL_EXPORTER_OTLP_HEADERS")
    assert_equal "Authorization=Bearer <key>", JSON.parse(telemetry["managed_settings"]).dig("env", "OTEL_EXPORTER_OTLP_HEADERS")
    assert_not telemetry["has_key"]
    assert_equal project.slug, telemetry["project_slug"]
  end

  test "with Agent sessions off the page says that nothing uploads" do
    sign_in_as(@member)

    page = setup_page

    assert_not page["on"]
    assert_equal [], page["captured_repositories"]
    assert_nil page["telemetry"]
  end

  test "someone outside the Organization gets 404" do
    sign_in_as(create(:user))

    get capture_setup_path(org_slug: @organization.slug)

    assert_response :not_found
  end

  private

  def with_env(values)
    previous = values.keys.index_with { |key| ENV[key] }
    values.each { |key, value| ENV[key] = value }
    yield
  ensure
    previous.each { |key, value| ENV[key] = value }
  end

  def turn_on(entries)
    AgentSessionsSettings.turn_on(organization: @organization, user: @admin, entries: entries, confirmed: true)
  end

  def setup_page
    get capture_setup_path(org_slug: @organization.slug),
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "Settings/CaptureSetup", response.parsed_body["component"]
    response.parsed_body["props"]
  end
end
