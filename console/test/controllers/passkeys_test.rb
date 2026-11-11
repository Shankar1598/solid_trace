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
    assert_redirected_to user_settings_url
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

  test "a Passkey made without user verification isn't added" do
    sign_in_as @user

    add_passkey(user_verified: false)

    assert_equal "That passkey couldn't be added. Try again.", flash[:alert]
    assert_empty user_settings_page["passkeys"]
  end
end
