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

  private

  def save(**settings)
    patch instance_settings_url(org_slug: @organization.slug), params: { instance_settings: settings }, as: :json
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
