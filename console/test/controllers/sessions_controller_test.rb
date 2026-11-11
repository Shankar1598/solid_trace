# frozen_string_literal: true

require "test_helper"

class SessionsControllerTest < ActionDispatch::IntegrationTest
  include SessionCookieHelper

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

  test "without Remember me, a Session lasts 24 hours from signing in, then signing in again returns to the page" do
    freeze_time
    post login_url, params: { email: @user.email, password: "password123" }

    assert_equal 24.hours.from_now, session_cookie_expires
    copied_cookie = { "Cookie" => "session_id=#{cookies[:session_id]}" }

    travel 24.hours - 1.minute
    get user_settings_url
    assert_response :success

    travel 2.minutes
    get user_settings_url, headers: copied_cookie
    assert_redirected_to login_url, "Console refuses the Session, not only the browser's cookie expiry"

    post login_url, params: { email: @user.email, password: "password123" }
    assert_redirected_to user_settings_url
  end

  test "a Session signed in before Sessions expired keeps working until the expiry it was given, then is refused and ended" do
    freeze_time
    session_record = @user.sessions.create!(expires_at: 7.days.from_now)
    cookies[:session_id] = permanent_signed_cookie(session_record.id)

    travel 7.days - 1.minute
    get user_settings_url
    assert_response :success

    travel 2.minutes
    get user_settings_url
    assert_redirected_to login_url
    assert_not Session.exists?(session_record.id), "the expired Session is ended"
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

  # The cookie signing in set before Sessions expired: signed, for 20 years.
  def permanent_signed_cookie(value)
    jar = ActionDispatch::Request.new(Rails.application.env_config.deep_dup).cookie_jar
    jar.signed.permanent[:session_id] = value
    jar[:session_id]
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
