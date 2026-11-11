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

  private

  def recovery_codes_page
    get recovery_codes_url, headers: inertia
    assert_equal "Settings/RecoveryCodes", response.parsed_body["component"]
    response.parsed_body["props"]
  end
end
