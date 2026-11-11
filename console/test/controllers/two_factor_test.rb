# frozen_string_literal: true

require "test_helper"

# Two-factor with an Authenticator app, through Console's pages: setting it
# up on its set-up page, the second step of signing in, and removing it.
# Codes are computed from the secret the set-up page shows, as the app would.
class TwoFactorTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
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

  private

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
