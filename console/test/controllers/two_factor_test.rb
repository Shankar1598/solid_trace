# frozen_string_literal: true

require "test_helper"

# Two-factor with an Authenticator app, through Console's pages: setting it
# up on its set-up page, the second step of signing in, and removing it.
# Codes are computed from the secret the set-up page shows, as the app would.
class TwoFactorTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include SessionCookieHelper

  # Time stands still unless a test moves it, so a code's step is certain.
  setup do
    freeze_time
    @user = create(:user, email: "ada@example.com")
  end

  test "the set-up page shows a QR code and the secret, and the app counts only once a code confirms it" do
    sign_in_as @user
    page = set_up_page
    assert_match %r{\A<svg}, page["qr_code"]
    assert_match %r{\Aotpauth://totp/SolidTrace:ada%40example\.com\?secret=#{page["secret"]}&issuer=SolidTrace\z}, page["uri"]

    # Not confirmed yet: the password alone still signs in.
    delete session_url
    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to root_url

    # The page keeps the secret being set up, so a reload shows the same one.
    assert_equal page["secret"], set_up_page["secret"]
    post authenticator_app_url, params: { code: code_for(page["secret"]) }
    assert_redirected_to user_settings_url
    assert_equal "Two-factor is on.", flash[:notice]

    delete session_url
    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to second_step_url
    assert_predicate cookies[:session_id], :blank?
  end

  test "the second step signs in with a code, and back to the page that was being opened" do
    secret = with_authenticator_app

    get user_settings_url
    assert_redirected_to login_url
    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to second_step_url
    get second_step_url, headers: inertia
    assert_equal "Sessions/SecondStep", response.parsed_body["component"]

    post second_step_url, params: { code: code_for(secret) }
    assert_redirected_to user_settings_url
    get user_settings_url, headers: inertia
    assert_response :success
  end

  test "Remember me, ticked with the password, makes the Session the second step signs in to last 7 days" do
    secret = with_authenticator_app

    post login_url, params: { email: @user.email, password: "password", remember_me: "1" }
    post second_step_url, params: { code: code_for(secret) }
    assert_equal 7.days.from_now, session_cookie_expires

    delete session_url
    travel 30.seconds
    start_sign_in
    post second_step_url, params: { code: code_for(secret) }
    assert_equal 24.hours.from_now, session_cookie_expires
  end

  test "a wrong code makes no Session and leaves the step to try again" do
    secret = with_authenticator_app
    post login_url, params: { email: @user.email, password: "password" }

    post second_step_url, params: { code: wrong_code(secret) }
    assert_redirected_to second_step_url
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    assert_predicate cookies[:session_id], :blank?

    post second_step_url, params: { code: code_for(secret) }
    assert_redirected_to root_url
    assert_predicate cookies[:session_id], :present?
  end

  test "a code is accepted for one step either side of now, not two" do
    secret = with_authenticator_app

    # Each try is minutes after the last, so no step was accepted before.
    [ -30, 30 ].each do |drift|
      travel 2.minutes
      start_sign_in
      post second_step_url, params: { code: code_for(secret, at: Time.current + drift) }
      assert_redirected_to root_url, "a code #{drift} s off"
      delete session_url
    end

    [ -60, 60 ].each do |drift|
      travel 2.minutes
      start_sign_in
      post second_step_url, params: { code: code_for(secret, at: Time.current + drift) }
      assert_redirected_to second_step_url, "a code #{drift} s off"
    end
  end

  test "no step is accepted twice, nor one before the last accepted" do
    secret = with_authenticator_app
    post login_url, params: { email: @user.email, password: "password" }
    post second_step_url, params: { code: code_for(secret) }
    assert_redirected_to root_url

    delete session_url
    start_sign_in
    post second_step_url, params: { code: code_for(secret) }
    assert_redirected_to second_step_url
    post second_step_url, params: { code: code_for(secret, at: 30.seconds.ago) }
    assert_redirected_to second_step_url

    post second_step_url, params: { code: code_for(secret, at: 30.seconds.from_now) }
    assert_redirected_to root_url
  end

  test "the pending step expires after five minutes" do
    secret = with_authenticator_app
    post login_url, params: { email: @user.email, password: "password" }

    travel 5.minutes + 1.second
    post second_step_url, params: { code: code_for(secret) }
    assert_redirected_to login_url
    assert_equal "That sign-in took too long. Sign in again.", flash[:alert]
    assert_predicate cookies[:session_id], :blank?

    get second_step_url
    assert_redirected_to login_url
  end

  test "a new sign-in clears the pending step" do
    secret = with_authenticator_app
    post login_url, params: { email: @user.email, password: "password" }
    post login_url, params: { email: @user.email, password: "wrong" }

    post second_step_url, params: { code: code_for(secret) }
    assert_redirected_to login_url
    assert_predicate cookies[:session_id], :blank?
  end

  test "the second step is never open without a right password first" do
    get second_step_url
    assert_redirected_to login_url

    with_authenticator_app
    post login_url, params: { email: @user.email, password: "wrong" }
    get second_step_url
    assert_redirected_to login_url
  end

  test "too many tries at the second step are told to wait, even with a right code" do
    secret = with_authenticator_app
    post login_url, params: { email: @user.email, password: "password" }

    SecondStepsController.cache_store.stub(:increment, 11) do
      post second_step_url, params: { code: code_for(secret) }
    end
    assert_redirected_to second_step_url
    assert_equal "Too many tries. Try again in a few minutes.", flash[:alert]
    assert_predicate cookies[:session_id], :blank?
  end

  test "getting Two-factor ends the user's other Sessions and keeps this one" do
    other_browser = open_session
    other_browser.post login_url, params: { email: @user.email, password: "password" }
    other_browser.get user_settings_url, headers: inertia
    other_browser.assert_response :success

    sign_in_as @user
    secret = set_up_page["secret"]
    post authenticator_app_url, params: { code: wrong_code(secret) }
    assert_redirected_to new_authenticator_app_url
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    other_browser.get user_settings_url, headers: inertia
    other_browser.assert_response :success

    post authenticator_app_url, params: { code: code_for(secret) }
    other_browser.get user_settings_url, headers: inertia
    other_browser.assert_redirected_to login_url
    get user_settings_url, headers: inertia
    assert_response :success
  end

  test "the set-up page returns to the Organization it is told of, once set up" do
    organization = create(:organization)
    organization.users << @user
    sign_in_as @user

    get new_authenticator_app_url(org: organization.slug), headers: inertia
    assert_equal({ "name" => organization.name, "slug" => organization.slug }, response.parsed_body.dig("props", "return_organization"))
    secret = response.parsed_body.dig("props", "secret")

    post authenticator_app_url, params: { code: wrong_code(secret), org: organization.slug }
    assert_redirected_to new_authenticator_app_url(org: organization.slug)
    post authenticator_app_url, params: { code: code_for(secret), org: organization.slug }
    assert_redirected_to overview_url(org_slug: organization.slug)

    # Another's Organization isn't named or returned to.
    get new_authenticator_app_url(org: create(:organization).slug)
    assert_redirected_to user_settings_url
  end

  test "Settings → User says whether the Authenticator app is set up, outside and inside an Organization" do
    organization = create(:organization)
    organization.users << @user
    sign_in_as @user

    get user_settings_url, headers: inertia
    assert_nil response.parsed_body.dig("props", "authenticator_app")

    secret = set_up_page["secret"]
    get user_settings_url, headers: inertia
    assert_nil response.parsed_body.dig("props", "authenticator_app"), "being set up isn't set up"

    post authenticator_app_url, params: { code: code_for(secret) }
    get user_path(org_slug: organization.slug), headers: inertia
    assert_equal Time.current.iso8601, response.parsed_body.dig("props", "authenticator_app", "set_up_at")
    assert_not response.parsed_body.to_s.include?(secret), "the secret is shown only while setting up"
  end

  test "removing the Authenticator app needs a code in the same request, and then a password alone signs in" do
    secret = with_authenticator_app
    sign_in_as @user
    travel 30.seconds

    delete authenticator_app_url, params: {}
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    delete authenticator_app_url, params: { code: wrong_code(secret) }
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    delete session_url
    start_sign_in

    post second_step_url, params: { code: code_for(secret) }
    assert_redirected_to root_url
    travel 30.seconds
    delete authenticator_app_url, params: { code: code_for(secret) }
    assert_redirected_to user_settings_url
    assert_equal "Authenticator app removed. Two-factor is off.", flash[:notice]

    delete session_url
    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to root_url
  end

  test "a new phone: the app is removed and set up again with a new secret" do
    secret = with_authenticator_app
    sign_in_as @user
    travel 30.seconds
    delete authenticator_app_url, params: { code: code_for(secret) }

    new_secret = set_up_page["secret"]
    assert_not_equal secret, new_secret
    post authenticator_app_url, params: { code: code_for(new_secret) }
    assert_equal "Two-factor is on.", flash[:notice]
  end

  test "the set-up page of a user who has the app sends them back to Settings → User" do
    with_authenticator_app
    sign_in_as @user

    get new_authenticator_app_url
    assert_redirected_to user_settings_url
    assert_equal "Your authenticator app is already set up.", flash[:notice]
  end

  test "the secret is encrypted at rest" do
    secret = with_authenticator_app

    stored = User.connection.select_value("SELECT authenticator_secret FROM users WHERE id = #{@user.id}")
    assert_not_includes stored, secret
  end

  test "a password reset keeps Two-factor, and still ends Sessions and revokes API tokens" do
    secret = with_authenticator_app
    sign_in_as @user
    ApiToken.issue!(user: @user, name: "Laptop")

    put password_url(@user.reload.password_reset_token), params: { password: "new-password", password_confirmation: "new-password" }
    assert_redirected_to login_url
    get user_settings_url, headers: inertia
    assert_redirected_to login_url
    assert_empty @user.api_tokens.reload

    post login_url, params: { email: @user.email, password: "new-password" }
    assert_redirected_to second_step_url
    travel 30.seconds
    post second_step_url, params: { code: code_for(secret) }
    assert_redirected_to user_settings_url
  end

  private

  # Sets up the Authenticator app as the user would, signs out, and moves on
  # to the next step, as the code that confirmed it is used up. Returns the
  # secret.
  def with_authenticator_app
    sign_in_as @user
    secret = set_up_page["secret"]
    post authenticator_app_url, params: { code: code_for(secret) }
    assert_equal "Two-factor is on.", flash[:notice]
    delete session_url
    travel 30.seconds
    secret
  end

  def start_sign_in
    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to second_step_url
  end

  # A code from another secret, so it is wrong for any step near now.
  def wrong_code(secret)
    (code_for(secret).to_i + 1).to_s.rjust(6, "0").last(6)
  end

  def set_up_page
    get new_authenticator_app_url, headers: inertia
    assert_equal "Settings/TwoFactorSetUp", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def code_for(secret, at: Time.current)
    ROTP::TOTP.new(secret).at(at)
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
