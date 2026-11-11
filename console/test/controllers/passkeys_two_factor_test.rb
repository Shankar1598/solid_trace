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
    add_passkey(name: "Phone", client: WebAuthn::FakeClient.new(CONSOLE_URL), proof: { credential: passkey_proof })
    assert_redirected_to user_settings_url
    assert_equal "Passkey added.", flash[:notice]
    other_browser.get user_settings_url, headers: inertia
    other_browser.assert_response :success
    assert_equal 9, user_settings_page["recovery_codes_left"]
  end

  test "a first Passkey needs the user's password in the same request, and a further one a Recovery code, a code or a Passkey" do
    sign_in_as @user

    add_passkey(name: "MacBook", client: WebAuthn::FakeClient.new(CONSOLE_URL), proof: {})
    assert_equal "That password didn't work.", flash[:alert]
    add_passkey(name: "MacBook", client: WebAuthn::FakeClient.new(CONSOLE_URL), proof: { password: "wrong" })
    assert_equal "That password didn't work.", flash[:alert]
    assert_empty user_settings_page["passkeys"]
    add_passkey(name: "MacBook")
    assert_redirected_to recovery_codes_url

    # Once the user has Two-factor, a stolen Session and password aren't enough.
    add_passkey(name: "Phone", client: WebAuthn::FakeClient.new(CONSOLE_URL), proof: { password: "password" })
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    assert_equal [ "MacBook" ], user_settings_page["passkeys"].pluck("name")
    phone = WebAuthn::FakeClient.new(CONSOLE_URL)
    add_passkey(name: "Phone", client: phone, proof: { recovery_code: @recovery_codes.first })
    assert_redirected_to user_settings_url
    add_passkey(name: "Tablet", client: WebAuthn::FakeClient.new(CONSOLE_URL), proof: { credential: passkey_proof(phone) })
    assert_redirected_to user_settings_url
    assert_equal [ "MacBook", "Phone", "Tablet" ], user_settings_page["passkeys"].pluck("name")
    assert_equal 9, user_settings_page["recovery_codes_left"]
  end

  test "a user with a Passkey sets up the Authenticator app for a Recovery code or a Passkey, not their password" do
    sign_in_as @user
    add_passkey
    secret = set_up_page["secret"]

    post authenticator_app_url, params: { code: code_for(secret), proof: { password: "password" } }
    assert_redirected_to new_authenticator_app_url
    assert_nil user_settings_page["authenticator_app"]
    # A wrong code from the app doesn't use the Recovery code up.
    post authenticator_app_url, params: { code: wrong_code(secret), proof: { recovery_code: @recovery_codes.first } }
    assert_redirected_to new_authenticator_app_url
    assert_equal 10, user_settings_page["recovery_codes_left"]

    post authenticator_app_url, params: { code: code_for(secret), proof: { credential: passkey_proof } }, as: :json
    assert_redirected_to user_settings_url
    assert_equal "Two-factor is on.", flash[:notice]
    assert_not_nil user_settings_page["authenticator_app"]
  end

  test "a Passkey added after the Authenticator app ends no Session and keeps the Recovery codes" do
    secret = with_authenticator_app
    other_browser = browser_at_console
    other_browser.post login_url, params: { email: @user.email, password: "password" }
    other_browser.post second_step_url, params: { recovery_code: @recovery_codes.first }
    sign_in_as @user

    add_passkey(proof: { code: code_for(secret, at: 30.seconds.from_now) })

    assert_redirected_to user_settings_url
    other_browser.get user_settings_url, headers: inertia
    other_browser.assert_response :success
    assert_equal 9, user_settings_page["recovery_codes_left"]
  end

  test "a Passkey is removed for a code, a Passkey or a Recovery code, and not for a wrong one" do
    secret = with_authenticator_app
    sign_in_as @user
    tablet = WebAuthn::FakeClient.new(CONSOLE_URL)
    stranger = WebAuthn::FakeClient.new(CONSOLE_URL)
    stranger.create(challenge: "unregistered", rp_id: "console.example.com", user_verified: true)
    add_passkey(name: "MacBook", proof: { code: code_for(secret, at: 30.seconds.from_now) })
    add_passkey(name: "Phone", client: WebAuthn::FakeClient.new(CONSOLE_URL), proof: { credential: passkey_proof })
    add_passkey(name: "Tablet", client: tablet, proof: { credential: passkey_proof })
    macbook, phone, tablet_id = user_settings_page["passkeys"].pluck("id")
    travel 60.seconds

    delete passkey_url(macbook), params: { code: wrong_code(secret) }
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    delete passkey_url(macbook), params: { credential: stranger.get(challenge: proof_challenge, user_verified: true) }, as: :json
    assert_equal "That passkey didn't work. Try again.", flash[:alert]
    delete passkey_url(macbook)
    assert_equal "That code didn't work. Try the newest one from your app.", flash[:alert]
    assert_equal 3, user_settings_page["passkeys"].size

    delete passkey_url(macbook), params: { code: code_for(secret) }
    assert_equal "Passkey removed.", flash[:notice]
    # The proof asks for any of the user's Passkeys here, the one going too.
    delete passkey_url(phone), params: { credential: passkey_proof(tablet) }, as: :json
    assert_equal "Passkey removed.", flash[:notice]
    delete passkey_url(tablet_id), params: { credential: passkey_proof(tablet) }, as: :json
    assert_equal "Passkey removed.", flash[:notice]
    assert_empty user_settings_page["passkeys"]
    assert_equal 10, user_settings_page["recovery_codes_left"]
  end

  test "removing the last factor turns Two-factor off and throws the Recovery codes away" do
    sign_in_as @user
    add_passkey
    passkey = user_settings_page["passkeys"].sole["id"]
    codes = @recovery_codes

    delete passkey_url(passkey), params: { recovery_code: codes.first }

    assert_equal "Passkey removed. Two-factor is off.", flash[:notice]
    assert_nil user_settings_page["recovery_codes_left"]
    delete session_url
    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to root_url
  end

  test "the Authenticator app is removed, and new Recovery codes made, for a Passkey" do
    secret = with_authenticator_app
    sign_in_as @user
    add_passkey(proof: { code: code_for(secret, at: 30.seconds.from_now) })

    post proof_options_passkeys_url, as: :json
    assert_equal 1, response.parsed_body["allowCredentials"].size
    post recovery_codes_url, params: { credential: passkey_proof }, as: :json
    assert_redirected_to recovery_codes_url
    delete authenticator_app_url, params: { credential: passkey_proof }, as: :json
    assert_equal "Authenticator app removed.", flash[:notice]
    assert_nil user_settings_page["authenticator_app"]
    assert_equal 10, user_settings_page["recovery_codes_left"]

    # A password alone still doesn't sign in: the Passkey is Two-factor.
    delete session_url
    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to second_step_url
  end

  test "a Passkey's last factor isn't removed while an Organization of the user's requires Two-factor" do
    organization = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: organization, user: @user, role: :member)
    sign_in_as @user
    add_passkey
    organization.update!(require_two_factor: true)
    passkey = user_settings_page["passkeys"].sole["id"]

    delete passkey_url(passkey), params: { credential: passkey_proof }, as: :json
    assert_equal "Acme requires Two-factor, so you can't remove your last factor.", flash[:alert]
    delete passkey_url(passkey), params: { recovery_code: @recovery_codes.first }
    assert_equal "Acme requires Two-factor, so you can't remove your last factor.", flash[:alert]

    assert_equal 1, user_settings_page["passkeys"].size
    assert_equal 10, user_settings_page["recovery_codes_left"]
  end

  test "a Passkey meets an Organization's Require two-factor, and counts for its admins and members" do
    organization = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: organization, user: @user, role: :admin)
    sign_in_as @user
    add_passkey
    get organization_path(org_slug: "acme"), headers: inertia
    assert_equal({ "on" => false, "can_change" => true, "you_have_two_factor" => true, "members_without_two_factor" => 0 },
      response.parsed_body.dig("props", "require_two_factor"))

    patch organization_path(org_slug: "acme"), params: { organization: { require_two_factor: true } }
    assert_redirected_to organization_path(org_slug: "acme")
    get overview_url(org_slug: "acme"), headers: inertia
    assert_response :success
  end

  test "a member sent to set up Two-factor is offered a Passkey, and one lets them into the Organization" do
    organization = create(:organization, name: "Acme", slug: "acme", require_two_factor: true)
    create(:organization_user, organization: organization, user: @user, role: :member)
    sign_in_as @user
    get overview_url(org_slug: "acme")
    assert_redirected_to new_authenticator_app_url(org: "acme")
    assert_equal true, set_up_page["passkeys_here"]

    # Settings → User, opened from the set-up page, keeps the Organization,
    # and Continue after the Recovery codes returns to it.
    get user_settings_url(org: "acme"), headers: inertia
    assert_equal({ "name" => "Acme", "slug" => "acme" }, response.parsed_body.dig("props", "return_organization"))
    add_passkey(org: "acme")
    assert_redirected_to recovery_codes_url
    assert_equal overview_path(org_slug: "acme"), recovery_codes_page["continue_to"]

    get overview_url(org_slug: "acme"), headers: inertia
    assert_response :success
  end

  test "a Passkey made for another host still counts, is listed as unusable here, and is removed with a Recovery code" do
    sign_in_as @user
    add_passkey(name: "MacBook")
    delete session_url

    open_console_at "https://solid-trace.example.ts.net"
    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to second_step_url
    get second_step_url, headers: inertia
    assert_equal({ "authenticator_app" => false, "passkey" => false }, response.parsed_body["props"].slice("authenticator_app", "passkey"))
    post second_step_url, params: { recovery_code: @recovery_codes.first }
    assert_redirected_to root_url

    passkey = user_settings_page["passkeys"].sole
    assert_equal [ "MacBook", "console.example.com", true ], passkey.values_at("name", "host", "other_host")
    delete passkey_url(passkey["id"]), params: { recovery_code: @recovery_codes.second }
    assert_equal "Passkey removed. Two-factor is off.", flash[:notice]
    assert_empty user_settings_page["passkeys"]
  end

  private

  def proof_challenge
    post proof_options_passkeys_url, as: :json
    response.parsed_body["challenge"]
  end

  # Another browser, opened at the Console URL.
  def browser_at_console
    open_session do |browser|
      browser.host! "console.example.com"
      browser.https!
    end
  end
end
