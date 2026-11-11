# frozen_string_literal: true

# Two-factor through Console's pages, as the user's Authenticator app would
# answer: codes computed from the secret the set-up page shows. Needs
# @user, and time frozen so a code's step is certain.
module TwoFactorTestHelper
  private

  # Sets up the Authenticator app as the user would, keeps the Recovery
  # codes it shows in @recovery_codes, signs out, and moves on to the next
  # step, as the code that confirmed it is used up. Returns the secret.
  def with_authenticator_app
    sign_in_as @user
    secret = set_up_page["secret"]
    post authenticator_app_url, params: { code: code_for(secret), proof: { password: "password" } }
    assert_equal "Two-factor is on.", flash[:notice]
    @recovery_codes = recovery_codes_page["codes"]
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

  def recovery_codes_page
    get recovery_codes_url, headers: inertia
    assert_equal "Settings/RecoveryCodes", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def code_for(secret, at: Time.current)
    ROTP::TOTP.new(secret).at(at)
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
