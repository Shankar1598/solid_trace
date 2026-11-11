# frozen_string_literal: true

require "test_helper"

# Two-factor with an Authenticator app, through Console's pages: setting it
# up on its set-up page, the second step of signing in, and removing it.
# Codes are computed from the secret the set-up page shows, as the app would.
class TwoFactorTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

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
