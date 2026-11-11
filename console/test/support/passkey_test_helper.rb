# frozen_string_literal: true

require "webauthn/fake_client"

# Passkeys through Console's pages, as a browser and its authenticator would
# answer: WebAuthn::FakeClient, at the Console URL's origin. Needs @user.
module PasskeyTestHelper
  CONSOLE_URL = "https://console.example.com"

  private

  # Sets the Console URL, so Passkeys are offered at its host, and sends the
  # test's requests there, as a page opened at it would.
  def open_console_at(url = CONSOLE_URL)
    InstanceSetting.current.update!(console_url: url)
    send_requests_to(url)
  end

  def send_requests_to(url)
    uri = URI(url)
    host! uri.port == uri.default_port ? uri.host : "#{uri.host}:#{uri.port}"
    https!(uri.scheme == "https")
  end

  # The browser's authenticator, which keeps the Passkeys it makes.
  def passkey_client(origin = CONSOLE_URL)
    @passkey_clients ||= {}
    @passkey_clients[origin] ||= WebAuthn::FakeClient.new(origin)
  end

  # Settings → User's "Add a passkey": the options, the authenticator's
  # answer, and the Passkey's name. Returns the options.
  def add_passkey(name: "MacBook", client: passkey_client, user_verified: true)
    options = creation_options
    credential = client.create(challenge: options["challenge"], rp_id: options.dig("rp", "id"), user_verified: user_verified)
    post passkeys_url, params: { name: name, credential: credential }, as: :json
    options
  end

  def creation_options
    post creation_options_passkeys_url, as: :json
    assert_response :success
    response.parsed_body
  end

  # The sign-in page's "Sign in with a passkey". get_options go to the
  # authenticator (sign_count:, rp_id:, user_verified:).
  def sign_in_with_passkey(client: passkey_client, remember_me: false, **get_options)
    post passkey_sign_in_options_url, as: :json
    assert_response :success
    options = response.parsed_body
    credential = client.get(challenge: options["challenge"], rp_id: options["rpId"], user_verified: true, **get_options)
    post passkey_sign_in_url, params: { credential: credential, remember_me: remember_me }, as: :json
  end

  def user_settings_page
    get user_settings_url, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_equal "Settings/User", response.parsed_body["component"]
    response.parsed_body["props"]
  end
end
