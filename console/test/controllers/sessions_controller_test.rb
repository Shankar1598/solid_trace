# frozen_string_literal: true

require "test_helper"

class SessionsControllerTest < ActionDispatch::IntegrationTest
  setup do
    @user = User.create!(name: "Test User", email: "test@example.com", password: "password123")
  end

  test "should login and logout" do
    # Login
    post login_url, params: { email: @user.email, password: "password123" }
    assert_redirected_to root_url
    session_id = cookies[:session_id]
    assert_not_nil session_id

    # Logout
    delete session_url
    assert_redirected_to login_url

    # Verify session is destroyed
    assert_predicate cookies[:session_id], :blank?
    assert_nil Session.find_by(id: session_id)

    # Verify we can't access protected page
    get root_url
    assert_redirected_to login_url
  end

  test "an email in another case signs in" do
    post login_url, params: { email: "Test@Example.COM", password: "password123" }

    assert_redirected_to root_url
    assert_not_nil cookies[:session_id]
  end

  test "too many tries are sent back to log in, told to wait, even with the right password" do
    SessionsController.cache_store.stub(:increment, 11) do
      post login_url, params: { email: @user.email, password: "password123" }
    end

    assert_redirected_to login_url
    assert_equal "Too many tries. Try again in a few minutes.", flash[:alert]
    assert_predicate cookies[:session_id], :blank?
  end

  test "the page offers sign-up only while it is allowed" do
    get login_url, headers: inertia
    assert_equal false, response.parsed_body.dig("props", "sign_up_allowed")

    InstanceSetting.current.update!(sign_up_open: true)
    get login_url, headers: inertia
    assert_equal true, response.parsed_body.dig("props", "sign_up_allowed")
  end

  private

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
