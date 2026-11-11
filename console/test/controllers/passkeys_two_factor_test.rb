# frozen_string_literal: true

require "test_helper"

# A Passkey counts as Two-factor (ADR 0018), through Console's pages: the
# second step after a password, a first Passkey's Recovery codes and ended
# Sessions, proving it's the user with a Passkey, removing Passkeys, Require
# two-factor, and Passkeys made for another host. The browser's
# authenticator is WebAuthn::FakeClient at the Console URL.
class PasskeysTwoFactorTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include TwoFactorTestHelper
  include SessionCookieHelper

  setup do
    freeze_time
    @user = create(:user, email: "ada@example.com", name: "Ada")
    open_console_at
  end

  test "a user with only a Passkey gives it after their password, and a password alone doesn't sign them in" do
    stranger = WebAuthn::FakeClient.new(CONSOLE_URL)
    sign_in_as create(:user, email: "grace@example.com")
    add_passkey(name: "Grace's", client: stranger)
    delete session_url
    sign_in_as @user
    add_passkey
    delete session_url

    post login_url, params: { email: @user.email, password: "password", remember_me: "1" }
    assert_redirected_to second_step_url
    assert_predicate cookies[:session_id], :blank?
    get second_step_url, headers: inertia
    assert_equal({ "authenticator_app" => false, "passkey" => true }, response.parsed_body["props"].slice("authenticator_app", "passkey"))

    # The step asks for this user's Passkeys alone, with user verification.
    post second_step_passkey_options_url, as: :json
    options = response.parsed_body
    assert_equal 1, options["allowCredentials"].size
    assert_equal "required", options["userVerification"]
    post second_step_url, params: { credential: stranger.get(challenge: options["challenge"], user_verified: true) }, as: :json
    assert_redirected_to second_step_url
    assert_equal "That passkey didn't work. Try again, or use a Recovery code.", flash[:alert]

    post second_step_passkey_options_url, as: :json
    challenge = response.parsed_body["challenge"]
    post second_step_url, params: { credential: passkey_client.get(challenge: challenge, user_verified: true) }, as: :json
    assert_redirected_to root_url
    assert_equal 7.days.from_now.to_i, session_cookie_expires.to_i
  end

  test "a first Passkey ends the user's other Sessions and shows Recovery codes once; a second ends nothing" do
    other_browser = browser_at_console
    other_browser.post login_url, params: { email: @user.email, password: "password" }
    sign_in_as @user

    add_passkey(name: "MacBook")

    assert_redirected_to recovery_codes_url
    assert_equal "Passkey added.", flash[:notice]
    page = recovery_codes_page
    assert_equal 10, page["codes"].size
    assert_equal user_settings_path, page["continue_to"]
    assert_equal 10, user_settings_page["recovery_codes_left"]
    other_browser.get user_settings_url, headers: inertia
    other_browser.assert_redirected_to login_url

    other_browser.post login_url, params: { email: @user.email, password: "password" }
    other_browser.post second_step_url, params: { recovery_code: page["codes"].first }
    add_passkey(name: "Phone", client: WebAuthn::FakeClient.new(CONSOLE_URL))
    assert_redirected_to user_settings_url
    assert_equal "Passkey added.", flash[:notice]
    other_browser.get user_settings_url, headers: inertia
    other_browser.assert_response :success
    assert_equal 9, user_settings_page["recovery_codes_left"]
  end

  test "a Passkey added after the Authenticator app ends no Session and keeps the Recovery codes" do
    with_authenticator_app
    other_browser = browser_at_console
    other_browser.post login_url, params: { email: @user.email, password: "password" }
    other_browser.post second_step_url, params: { recovery_code: @recovery_codes.first }
    sign_in_as @user

    add_passkey

    assert_redirected_to user_settings_url
    other_browser.get user_settings_url, headers: inertia
    other_browser.assert_response :success
    assert_equal 9, user_settings_page["recovery_codes_left"]
  end

  private

  # Another browser, opened at the Console URL.
  def browser_at_console
    open_session do |browser|
      browser.host! "console.example.com"
      browser.https!
    end
  end
end
