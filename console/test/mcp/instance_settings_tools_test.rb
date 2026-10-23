# frozen_string_literal: true

require "test_helper"

# The instance settings tools: the install's settings as Settings → Instance
# shows them, and the three that apply at once, which only an instance admin
# reads or changes. They take no Organization, never answer a secret, and
# can't change Sign-up or anything that needs a restart.
class McpInstanceSettingsToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @acme = create(:organization, name: "Acme", slug: "acme")
    @instance_admin = create(:user, name: "Grace", email: "grace@example.com", instance_admin: true)
    create(:organization_user, organization: @acme, user: @instance_admin, role: :admin)
    @org_admin = create(:user, name: "Ada", email: "ada@example.com")
    create(:organization_user, organization: @acme, user: @org_admin, role: :admin)
  end

  test "get_instance_settings answers the page's sections, never a secret's value, and links to the page" do
    InstanceSetting.current.update!(sql_queries_enabled: false, default_log_retention_days: 14)

    answer = with_env("SECRET_KEY_BASE" => "k3y-base-v4lue", "SENTRY_DSN" => "https://s3cret@sentry.example/1") do
      call_tool(:get_instance_settings, user: @instance_admin)
    end

    assert_not answer.error?, answer.text
    assert_equal({ "sql_queries_enabled" => false, "sql_queries_description" => InstanceSetting::SQL_QUERIES_DESCRIPTION,
      "default_log_retention_days" => 14, "log_all_reads" => false }, answer.json["editable"])
    assert_equal({ "open" => false, "command" => "bin/rails instance_settings:set SIGN_UP_OPEN=true|false" },
      answer.json["sign_up"])
    console = answer.json["restart_to_change"].find { |group| group["name"] == "Console" }
    assert_equal [ true, true ], console["settings"].select { |row| row["secret"] }.pluck("value")
    assert_not_includes answer.text, "k3y-base-v4lue"
    assert_not_includes answer.text, "s3cret"
    assert_equal "http://console.test/acme/settings/instance", answer.link
  end

  test "update_instance_settings changes the settings given, and answers the settings" do
    answer = call_tool(:update_instance_settings, user: @instance_admin, sql_queries_enabled: false,
      default_log_retention_days: 7, log_all_reads: true)

    assert_not answer.error?, answer.text
    settings = InstanceSetting.current
    assert_equal [ false, 7, true ], [ settings.sql_queries_enabled, settings.default_log_retention_days, settings.log_all_reads ]
    assert_equal [ false, 7, true ],
      answer.json["editable"].values_at("sql_queries_enabled", "default_log_retention_days", "log_all_reads")
    assert_equal "http://console.test/acme/settings/instance", answer.link

    call_tool(:update_instance_settings, user: @instance_admin, log_all_reads: false)
    settings = InstanceSetting.current
    assert_equal [ false, 7, false ], [ settings.sql_queries_enabled, settings.default_log_retention_days, settings.log_all_reads ]
  end

  private

  def with_env(values)
    previous = values.keys.index_with { |key| ENV[key] }
    values.each { |key, value| ENV[key] = value }
    yield
  ensure
    previous.each { |key, value| ENV[key] = value }
  end
end
