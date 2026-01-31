# frozen_string_literal: true

require "application_system_test_case"
require "json"

class SmokeTest < ApplicationSystemTestCase
  test "visiting the login page" do
    visit login_url
    assert_selector "h1", text: /sign in|log in/i
  end

  test "health endpoint returns healthy" do
    # System test using Capybara's lower-level HTTP
    # For JSON endpoints, we typically use integration tests,
    # but this validates the endpoint is routable
    visit health_check_url
    assert page.status_code == 200

    json = JSON.parse(page.body)
    assert_equal "healthy", json["status"]
    assert json.key?("checks")
    assert json["checks"].dig("database", "status") == "ok"
    assert json["checks"].dig("event_store", "status") == "ok"
  end
end
