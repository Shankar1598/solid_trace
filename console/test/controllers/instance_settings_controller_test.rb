# frozen_string_literal: true

require "test_helper"

# Settings → Instance: the install's settings. Those that apply at once (SQL
# queries, the default Retention, log-all-reads) change here; Sign-up and
# everything that needs a restart are shown read-only. Only an instance admin
# sees or changes them.
class InstanceSettingsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @instance_admin = create(:user, instance_admin: true)
    @organization.organization_users.create!(user: @instance_admin, role: :admin)
    @org_admin = create(:user)
    @organization.organization_users.create!(user: @org_admin, role: :admin)
  end

  test "an Organization admin who isn't an instance admin is refused the page and its update" do
    sign_in_as(@org_admin)

    get instance_settings_url(org_slug: @organization.slug)
    assert_redirected_to root_url
    assert_equal "You are not authorized to perform this action.", flash[:alert]

    patch instance_settings_url(org_slug: @organization.slug),
      params: { instance_settings: { sql_queries_enabled: false, log_all_reads: true } }
    assert_redirected_to root_url
    assert InstanceSetting.current.sql_queries_enabled?
    assert_not InstanceSetting.current.log_all_reads?
  end

  test "an instance admin sees the settings that apply at once, Sign-up, and Console's environment" do
    InstanceSetting.current.update!(sql_queries_enabled: false, default_log_retention_days: 14)
    sign_in_as(@instance_admin)

    get instance_settings_url(org_slug: @organization.slug), headers: inertia

    assert_response :success
    assert_equal "Settings/Instance", response.parsed_body["component"]
    settings = response.parsed_body.dig("props", "settings")
    assert_equal({ "sql_queries_enabled" => false, "sql_queries_description" => InstanceSetting::SQL_QUERIES_DESCRIPTION,
      "default_log_retention_days" => 14, "log_all_reads" => false }, settings["editable"])
    assert_equal false, settings.dig("sign_up", "open")
    assert_equal "bin/rails instance_settings:set SIGN_UP_OPEN=true|false", settings.dig("sign_up", "command")

    console = settings["restart_to_change"].find { |group| group["name"] == "Console" }
    version = console["settings"].find { |row| row["source"] == "SOLIDTRACE_VERSION" }
    assert_equal({ "label" => "Version", "source" => "SOLIDTRACE_VERSION", "value" => "dev", "default" => true,
      "secret" => false }, version)
    assert_equal [ "CONSOLE_URL", "PUBLIC_INGEST_URL", "EVENT_STORE_URL", "EVENT_STORE_INGEST_URL", "SOLIDTRACE_VERSION",
      "RAILS_MAX_THREADS", "JOB_CONCURRENCY", "RAILS_LOG_LEVEL", "SECRET_KEY_BASE", "SENTRY_DSN" ],
      console["settings"].pluck("source")
  end

  test "a setting given in the environment says it isn't the default" do
    sign_in_as(@instance_admin)

    with_env("EVENT_STORE_URL" => "http://10.0.0.5:4100", "JOB_CONCURRENCY" => "3") do
      get instance_settings_url(org_slug: @organization.slug), headers: inertia
    end

    rows = console_rows
    assert_equal [ "http://10.0.0.5:4100", false ], rows["EVENT_STORE_URL"].values_at("value", "default")
    assert_equal [ "3", false ], rows["JOB_CONCURRENCY"].values_at("value", "default")
    assert_equal [ "http://127.0.0.1:4000", true ], rows["EVENT_STORE_INGEST_URL"].values_at("value", "default")
  end

  test "secrets show only whether they are set, never their value" do
    sign_in_as(@instance_admin)

    with_env("SECRET_KEY_BASE" => "k3y-base-v4lue", "SENTRY_DSN" => nil) do
      get instance_settings_url(org_slug: @organization.slug), headers: inertia
    end

    rows = console_rows
    assert_equal({ "label" => "Secret key base", "source" => "SECRET_KEY_BASE", "value" => true, "default" => false,
      "secret" => true }, rows["SECRET_KEY_BASE"])
    assert_equal [ false, true, true ], rows["SENTRY_DSN"].values_at("value", "default", "secret")
    assert_not_includes response.body, "k3y-base-v4lue"
  end

  private

  def console_rows
    response.parsed_body.dig("props", "settings", "restart_to_change")
      .find { |group| group["name"] == "Console" }["settings"].index_by { |row| row["source"] }
  end

  def with_env(values)
    previous = values.keys.index_with { |key| ENV[key] }
    values.each { |key, value| ENV[key] = value }
    yield
  ensure
    previous.each { |key, value| ENV[key] = value }
  end

  def save(**settings)
    patch instance_settings_url(org_slug: @organization.slug), params: { instance_settings: settings }, as: :json
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
