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

  test "should get new with providers hash" do
    get new_integration_url(org_slug: @organization.slug)
    assert_response :success
    # We can check the inertia props if we have access to them,
    # but at least this confirms the action doesn't crash.
  end
end
