# frozen_string_literal: true

require "test_helper"

class IntegrationsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user

    sign_in_as(@user)
  end

  test "should create slack integration with custom settings" do
    assert_difference -> { @organization.integrations.count } => 1 do
      post integrations_url(org_slug: @organization.slug), params: {
        integration: {
          provider: "slack",
          name: "Slack Alert",
          settings: {
            webhook_url: "https://hooks.slack.com/services/123",
            notify_on_new_issue: "1",
            notify_on_event_threshold: "1",
            event_threshold: "20",
            time_window_minutes: "10",
          },
        },
      }
    end

    assert_redirected_to integrations_url(org_slug: @organization.slug)
    integration = @organization.integrations.last
    assert_equal "slack", integration.provider
    assert_equal "20", integration.settings["event_threshold"]
  end

  test "should create email integration" do
    assert_difference -> { @organization.integrations.count } => 1 do
      post integrations_url(org_slug: @organization.slug), params: {
        integration: {
          provider: "email",
          name: "Email Alert",
          settings: {
            recipients: "test@example.com",
            notify_on_new_issue: "1",
          },
        },
      }
    end

    assert_redirected_to integrations_url(org_slug: @organization.slug)
    integration = @organization.integrations.last
    assert_equal "email", integration.provider
    assert_equal "test@example.com", integration.settings["recipients"]
  end

  test "should fail to create integration with invalid provider (like '0')" do
    assert_no_difference -> { @organization.integrations.count } do
      post integrations_url(org_slug: @organization.slug), params: {
        integration: {
          provider: "0",
          name: "Invalid",
          settings: {
            webhook_url: "https://hooks.slack.com/services/123",
          },
        },
      }
    end

    assert_response :redirect
    # Inertia redirect with errors
    assert_equal "is not included in the list", flash[:inertia_errors][:provider].first if flash[:inertia_errors]
  end

  test "the New page starts from the model's rule defaults" do
    get new_integration_url(org_slug: @organization.slug)

    assert_response :success
    assert_equal Integration.new.rule_settings, inertia_props["defaults"]
  end

  test "the Edit page shows the rule settings the model applies when none were saved" do
    integration = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/x" })

    get edit_integration_url(integration, org_slug: @organization.slug)

    settings = inertia_props.dig("integration", "settings")
    assert_equal "https://hooks.slack.com/x", settings["webhook_url"]
    assert_equal true, settings["notify_on_event_threshold"]
    assert_equal true, settings["notify_on_new_issue"]
    assert_equal false, settings["notify_on_assignment"]
    assert_equal 10, settings["event_threshold"]
    assert_equal 5, settings["time_window_minutes"]
  end

  private

  def inertia_props
    page = Nokogiri::HTML(response.body).at_css("[data-page]")
    json = page["data-page"].start_with?("{") ? page["data-page"] : page.text
    JSON.parse(json)["props"]
  end
end
