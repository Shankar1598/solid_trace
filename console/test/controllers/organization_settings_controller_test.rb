# frozen_string_literal: true

require "test_helper"

class OrganizationSettingsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user

    sign_in_as(@user)
  end

  test "show returns success" do
    get organization_path(org_slug: @organization.slug)

    assert_response :success
  end

  test "update organization name" do
    patch organization_path(org_slug: @organization.slug), params: {
      organization: { name: "New Org Name" }
    }

    assert_redirected_to organization_path(org_slug: @organization.slug)
    assert_equal "New Org Name", @organization.reload.name
  end

  test "update organization slug" do
    patch organization_path(org_slug: @organization.slug), params: {
      organization: { slug: "new-slug" }
    }

    assert_redirected_to organization_path(org_slug: "new-slug")
    assert_equal "new-slug", @organization.reload.slug
  end

  test "a member chooses the Monitor status Integration from the Slack, PagerDuty and email ones, or None" do
    slack = @organization.integrations.create!(provider: "slack", name: "Ops", settings: { "webhook_url" => "https://hooks.slack.com/x" })
    factory = @organization.integrations.create!(provider: "factory", name: "Fixer")

    get organization_path(org_slug: @organization.slug)
    setting = inertia_props["monitor_status"]
    assert_nil setting["integration_id"]
    assert_equal [ "Ops" ], setting["options"].map { |option| option["name"] }

    patch organization_path(org_slug: @organization.slug), params: { organization: { monitor_status_integration_id: slack.id } }
    assert_equal slack, @organization.reload.monitor_status_integration

    patch organization_path(org_slug: @organization.slug), params: { organization: { monitor_status_integration_id: factory.id } }
    assert_equal slack, @organization.reload.monitor_status_integration

    patch organization_path(org_slug: @organization.slug), params: { organization: { monitor_status_integration_id: "" } }
    assert_nil @organization.reload.monitor_status_integration_id
  end

  test "denies access to other org" do
    other_org = create(:organization)

    get organization_path(org_slug: other_org.slug)

    assert_response :not_found
  end

  private

  def inertia_props
    page = Nokogiri::HTML(response.body).at_css("[data-page]")
    json = page["data-page"].start_with?("{") ? page["data-page"] : page.text
    JSON.parse(json)["props"]
  end
end
