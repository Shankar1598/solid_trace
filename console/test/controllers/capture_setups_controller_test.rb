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

  test "the plugin install commands add the release's marketplace file and install solidtrace@solidtrace" do
    sign_in_as(@member)

    assert_equal [ "claude plugin marketplace add #{RELEASE}/marketplace.json", "claude plugin install solidtrace@solidtrace" ],
                 setup_page["plugin_commands"]
  end

  test "the managed-settings snippet registers the marketplace with a url source and force-enables the plugin" do
    sign_in_as(@admin)

    snippet = JSON.parse(setup_page["managed_settings"])

    assert_equal({ "extraKnownMarketplaces" => { "solidtrace" => { "source" => { "source" => "url", "url" => "#{RELEASE}/marketplace.json" } } },
                   "enabledPlugins" => { "solidtrace@solidtrace" => true } }, snippet)
  end

  test "an instance that runs no release gets the newest release's files" do
    Rails.application.config.x.solidtrace_version = "dev"
    sign_in_as(@member)

    page = setup_page

    latest = "https://github.com/Shankar1598/solid_trace/releases/latest/download"
    assert_equal "curl -fsSL #{latest}/install.sh | sh", page["install_command"]
    assert_equal "#{latest}/marketplace.json", JSON.parse(page["managed_settings"]).dig("extraKnownMarketplaces", "solidtrace", "source", "url")
    assert page["version_note"].present?
  end

  test "with no Console URL saved or set, the login command uses the address the request reached Console at" do
    InstanceSetting.current.update!(console_url: nil, ingest_url: nil)
    sign_in_as(@member)

    assert_equal "solidtrace login --ingest-url http://www.example.com:4000 http://www.example.com",
                 with_env("CONSOLE_URL" => nil, "PUBLIC_INGEST_URL" => nil) { setup_page["login_command"] }
  end

  test "with Agent sessions off the page says that nothing uploads" do
    sign_in_as(@member)

    page = setup_page

    assert_not page["on"]
    assert_equal [], page["captured_repositories"]
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
