# frozen_string_literal: true

require "test_helper"

# Passkeys through Console's pages: adding, renaming and removing them on
# Settings → User, and signing in with one alone. The browser's authenticator
# is WebAuthn::FakeClient, at the Console URL, where Passkeys are offered
# (ADR 0018).
class PasskeysTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include SessionCookieHelper

  setup do
    freeze_time
    @user = create(:user, email: "ada@example.com", name: "Ada")
    open_console_at
  end

  test "a user adds a Passkey for the Console URL's host, needing a discoverable credential and user verification" do
    sign_in_as @user

    options = add_passkey(name: "MacBook")

    assert_equal({ "id" => "console.example.com", "name" => "SolidTrace" }, options["rp"])
    assert_equal({ "residentKey" => "required", "requireResidentKey" => true, "userVerification" => "required" },
      options["authenticatorSelection"])
    assert_equal [ "ada@example.com", "Ada" ], options["user"].values_at("name", "displayName")
    assert_equal [], options["excludeCredentials"]
    assert_redirected_to recovery_codes_url
    assert_equal "Passkey added.", flash[:notice]

    page = user_settings_page
    assert_equal({ "offered" => true, "host" => "console.example.com", "url" => "https://console.example.com", "here" => true },
      page["passkey_offer"])
    assert_equal [ [ "MacBook", Time.current.iso8601, nil ] ],
      page["passkeys"].map { |passkey| passkey.values_at("name", "created_at", "last_used_at") }

    # The user keeps one user handle, and the next Passkey can't be made on
    # the same authenticator twice.
    again = creation_options
    assert_equal options.dig("user", "id"), again.dig("user", "id")
    assert_equal 1, again["excludeCredentials"].size
  end

  test "a Passkey alone signs its user in, with Remember me, even with an Authenticator app, and records when" do
    @user.update!(authenticator_secret: ROTP::Base32.random, authenticator_confirmed_at: Time.current)
    sign_in_as @user
    add_passkey(proof: { code: ROTP::TOTP.new(@user.authenticator_secret).at(30.seconds.from_now) })
    delete session_url

    sign_in_with_passkey(remember_me: true)

    assert_redirected_to root_url
    assert_equal 7.days.from_now.to_i, session_cookie_expires.to_i
    assert_equal [ Time.current.iso8601 ], user_settings_page["passkeys"].pluck("last_used_at")

    delete session_url
    sign_in_with_passkey
    assert_equal 24.hours.from_now.to_i, session_cookie_expires.to_i
  end

  test "the sign-in page says whether Passkeys work here, and where they do when not" do
    get login_url, headers: inertia_headers
    assert_equal({ "offered" => true, "host" => "console.example.com", "url" => "https://console.example.com", "here" => true },
      response.parsed_body.dig("props", "passkey_offer"))

    send_requests_to "http://10.164.0.3:3000"
    get login_url, headers: inertia_headers
    assert_equal false, response.parsed_body.dig("props", "passkey_offer", "here")
    post passkey_sign_in_options_url, as: :json
    assert_response :unprocessable_entity
    assert_equal "Passkeys work at https://console.example.com.", response.parsed_body["error"]
  end

  test "an answer with a sign count that went back doesn't sign in, as the Passkey may have been copied" do
    sign_in_as @user
    add_passkey
    delete session_url
    sign_in_with_passkey(sign_count: 5)
    assert_redirected_to root_url
    delete session_url

    sign_in_with_passkey(sign_count: 3)

    assert_redirected_to login_url
    assert_equal "That passkey didn't sign you in. Try again, or use your password.", flash[:alert]
    assert_predicate cookies[:session_id], :blank?
  end

  test "a Passkey made for the Console URL's old host doesn't sign in at the new one" do
    sign_in_as @user
    add_passkey
    delete session_url
    old = passkey_client

    open_console_at "https://solid-trace.example.ts.net"
    client = WebAuthn::FakeClient.new("https://solid-trace.example.ts.net", authenticator: old.send(:authenticator))
    sign_in_with_passkey(client: client, rp_id: "console.example.com")

    assert_redirected_to login_url
    assert_predicate cookies[:session_id], :blank?
    post login_url, params: { email: @user.email, password: "password" }
    post second_step_url, params: { recovery_code: @recovery_codes.first }
    assert_equal({ "offered" => true, "host" => "solid-trace.example.ts.net", "url" => "https://solid-trace.example.ts.net", "here" => true },
      user_settings_page["passkey_offer"])
  end

  test "signing in with a Passkey is rate-limited as signing in is" do
    PasskeySignInsController.cache_store.stub(:increment, 11) do
      post passkey_sign_in_url, params: { credential: {} }, as: :json
    end

    assert_redirected_to login_url
    assert_equal "Too many tries. Try again in a few minutes.", flash[:alert]
  end

  test "a user renames a Passkey" do
    sign_in_as @user
    add_passkey(name: "MacBook")
    add_passkey(name: "Phone", client: WebAuthn::FakeClient.new(CONSOLE_URL), proof: { credential: passkey_proof })
    macbook_id = user_settings_page["passkeys"].first["id"]

    patch passkey_url(macbook_id), params: { name: "  Work laptop " }, as: :json
    assert_equal "Passkey renamed.", flash[:notice]
    patch passkey_url(macbook_id), params: { name: "" }, as: :json
    assert_equal "Name can't be blank.", flash[:alert]
    assert_equal [ "Work laptop", "Phone" ], user_settings_page["passkeys"].pluck("name")
  end

  test "another user's Passkey can't be renamed or removed" do
    other = create(:user)
    passkey = other.passkeys.create!(credential_id: "abc", public_key: "def", name: "Theirs", host: "console.example.com")
    sign_in_as @user

    patch passkey_url(passkey), params: { name: "Mine" }, as: :json
    assert_response :not_found
    delete passkey_url(passkey), as: :json
    assert_response :not_found
    assert_equal "Theirs", passkey.reload.name
  end

  test "Settings → User opened at another address says where Passkeys work, and adds none there" do
    send_requests_to "http://10.164.0.3:3000"
    sign_in_as @user

    assert_equal({ "offered" => true, "host" => "console.example.com", "url" => "https://console.example.com", "here" => false },
      user_settings_page["passkey_offer"])
    post creation_options_passkeys_url, as: :json
    assert_response :unprocessable_entity
    assert_equal "Passkeys work at https://console.example.com.", response.parsed_body["error"]
  end

  test "a Passkey made without user verification isn't added" do
    sign_in_as @user

    add_passkey(user_verified: false)

    assert_equal "That passkey couldn't be added. Try again.", flash[:alert]
    assert_empty user_settings_page["passkeys"]
  end
end
