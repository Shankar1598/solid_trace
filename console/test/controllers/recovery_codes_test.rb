# frozen_string_literal: true

require "test_helper"

# Recovery codes, through Console's pages: shown once when the user first
# gets Two-factor, each accepted once in place of a code, counted on
# Settings → User, replaced by a new set, and thrown away with the last
# factor.
class RecoveryCodesTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include TwoFactorTestHelper

  setup do
    freeze_time
    @user = create(:user, email: "ada@example.com")
  end

  test "getting Two-factor shows 10 Recovery codes once, and Console keeps only their digests" do
    sign_in_as @user
    secret = set_up_page["secret"]
    post authenticator_app_url, params: { code: code_for(secret) }
    assert_redirected_to recovery_codes_url
    assert_equal "Two-factor is on.", flash[:notice]

    page = recovery_codes_page
    codes = page["codes"]
    assert_equal 10, codes.uniq.size
    codes.each { |code| assert_match(/\A[a-z0-9]{4}(-[a-z0-9]{4}){3}\z/, code) }
    assert_equal user_settings_path, page["continue_to"]

    get recovery_codes_url
    assert_redirected_to user_settings_url

    stored = RecoveryCode.connection.select_values("SELECT digest FROM recovery_codes")
    assert_equal 10, stored.size
    codes.each { |code| assert_not(stored.any? { |digest| digest.include?(code.delete("-")) }) }
  end

  test "the second step accepts a Recovery code once, in any case and spacing, and the notice says how many are left" do
    with_authenticator_app
    get user_settings_url
    start_sign_in

    post second_step_url, params: { recovery_code: " #{@recovery_codes.first.upcase.delete("-")} " }
    assert_redirected_to user_settings_url
    assert_equal "Signed in with a Recovery code. 9 left.", flash[:notice]

    delete session_url
    start_sign_in
    post second_step_url, params: { recovery_code: @recovery_codes.first }
    assert_redirected_to second_step_url
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    assert_predicate cookies[:session_id], :blank?
    post second_step_url, params: { recovery_code: "0000-0000-0000-0000" }
    assert_redirected_to second_step_url

    @recovery_codes.drop(1).take(7).each_with_index do |code, i|
      post second_step_url, params: { recovery_code: code }
      assert_redirected_to root_url, "code #{i + 2}"
      delete session_url
      start_sign_in
    end
    post second_step_url, params: { recovery_code: @recovery_codes[8] }
    assert_equal "Signed in with a Recovery code. 1 left.", flash[:notice]
    delete session_url
    start_sign_in
    post second_step_url, params: { recovery_code: @recovery_codes[9] }
    assert_equal "Signed in with your last Recovery code. Make new ones on Settings → User.", flash[:notice]
  end

  test "Settings → User says how many Recovery codes are left, and nothing without Two-factor" do
    sign_in_as @user
    get user_settings_url, headers: inertia
    assert_nil response.parsed_body.dig("props", "recovery_codes_left")
    delete session_url

    with_authenticator_app
    start_sign_in
    post second_step_url, params: { recovery_code: @recovery_codes.first }
    get user_settings_url, headers: inertia
    assert_equal 9, response.parsed_body.dig("props", "recovery_codes_left")
  end

  test "a new set needs a code or a Recovery code, replaces the old set, and is shown once" do
    secret = with_authenticator_app
    sign_in_as @user
    travel 30.seconds

    post recovery_codes_url, params: { code: wrong_code(secret) }
    assert_redirected_to user_settings_url
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    post recovery_codes_url, params: {}
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]

    post recovery_codes_url, params: { code: code_for(secret) }
    assert_redirected_to recovery_codes_url
    assert_equal "New Recovery codes made. The old ones no longer work.", flash[:notice]
    page = recovery_codes_page
    assert_equal 10, page["codes"].size
    assert_empty page["codes"] & @recovery_codes
    assert_equal user_settings_path, page["continue_to"]
    get recovery_codes_url
    assert_redirected_to user_settings_url

    # A Recovery code from the new set is proof for the next one.
    post recovery_codes_url, params: { recovery_code: page["codes"].first }
    newest = recovery_codes_page["codes"]
    get user_settings_url, headers: inertia
    assert_equal 10, response.parsed_body.dig("props", "recovery_codes_left")

    delete session_url
    start_sign_in
    post second_step_url, params: { recovery_code: @recovery_codes.second }
    assert_redirected_to second_step_url, "the first set no longer works"
    post second_step_url, params: { recovery_code: page["codes"].second }
    assert_redirected_to second_step_url, "the second set no longer works"
    post second_step_url, params: { recovery_code: newest.first }
    assert_redirected_to root_url
  end

  test "a Recovery code removes the last factor, which throws the Recovery codes away, and getting Two-factor again makes a new set" do
    with_authenticator_app
    sign_in_as @user

    delete authenticator_app_url, params: { recovery_code: @recovery_codes.first }
    assert_redirected_to user_settings_url
    assert_equal "Authenticator app removed. Two-factor is off.", flash[:notice]
    get user_settings_url, headers: inertia
    assert_nil response.parsed_body.dig("props", "recovery_codes_left")
    assert_equal 0, @user.recovery_codes.count

    secret = set_up_page["secret"]
    post authenticator_app_url, params: { code: code_for(secret) }
    assert_redirected_to recovery_codes_url
    fresh = recovery_codes_page["codes"]
    assert_empty fresh & @recovery_codes

    delete session_url
    start_sign_in
    post second_step_url, params: { recovery_code: @recovery_codes.second }
    assert_redirected_to second_step_url, "the thrown-away set no longer works"
    post second_step_url, params: { recovery_code: fresh.first }
    assert_redirected_to root_url
  end
end
