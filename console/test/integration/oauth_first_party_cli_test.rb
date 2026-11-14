# frozen_string_literal: true

require "test_helper"

# Seam 2: solidtrace-cli, the one OAuth client built into Console, which
# `solidtrace login` uses. It is public, and proves the code with PKCE on a
# loopback redirect at any port; it is the only client that is not
# published (CIMD) and still gets the device flow. No other client gets
# its loopback redirect or its device flow, and no registration or
# published description takes its client_id.
class OauthFirstPartyCliTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include OauthTestHelper

  CLI = "solidtrace-cli"
  FACTORY = "#{OauthTestHelper::ISSUER}/factory".freeze
  LOGIN_SCOPE = "sessions:write factory"
  CLI_REDIRECT = "http://127.0.0.1:53682/callback"
  DEVICE_GRANT = "urn:ietf:params:oauth:grant-type:device_code"
  PROGRAM_ADDRESS = "203.0.113.7"
  SESSIONS_WRITE_TEXT = "Upload Agent sessions to Acme's Agent Project, and read which parts of them it has."
  FACTORY_TEXT = "Use the Dispatcher as you, in Acme: start, message and stop Runs, and do the Admin actions " \
                 "that your role allows."
  FIRST_PARTY_NOTE = "Checked: the SolidTrace CLI, part of Console"

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    open_console_at
    sign_in_as @user
  end

  # The browser sign-in

  test "the approval page shows solidtrace-cli as first-party, with the text of sessions:write and factory" do
    props = cli_approval_page

    assert_equal({ "name" => "SolidTrace CLI", "name_checked" => true, "name_note" => FIRST_PARTY_NOTE,
                   "redirect_host" => "127.0.0.1" }, props["client"])
    assert_equal [ SESSIONS_WRITE_TEXT, FACTORY_TEXT ], props["organizations"].sole["scopes"]
    assert_equal "This program runs on a computer. Approve only if you started it.", props["loopback_warning"]
  end

  test "solidtrace-cli exchanges its code with PKCE for an access token and a refresh token, which rotates" do
    tokens = cli_tokens

    assert_equal [ "Bearer", 3600, LOGIN_SCOPE ], tokens.values_at("token_type", "expires_in", "scope")
    assert_match(/\Asto_/, tokens["access_token"])
    assert_match(/\Astr_/, tokens["refresh_token"])

    refresh tokens["refresh_token"]
    assert_response :ok
    rotated = response.parsed_body
    assert_not_equal tokens["refresh_token"], rotated["refresh_token"]
    assert_equal LOGIN_SCOPE, rotated["scope"]
    assert introspect(rotated["access_token"])["active"]

    travel 61.seconds
    refresh tokens["refresh_token"]
    assert_oauth_error "invalid_grant"
    assert_not introspect(rotated["access_token"])["active"], "a used refresh token revokes the sign-in"
  end

  test "the loopback redirect works at any port, and at localhost and [::1] too" do
    [ "http://127.0.0.1:1/callback", "http://127.0.0.1:65535/callback", "http://localhost:4000/callback",
      "http://[::1]:8080/callback", "http://127.0.0.1/callback" ].each do |redirect_uri|
      props = cli_approval_page(redirect_uri: redirect_uri)
      approve(props["request"], @acme)

      assert_response :conflict, redirect_uri
      assert response.headers["X-Inertia-Location"].start_with?("#{redirect_uri}?"), redirect_uri
      exchange(redirect_query["code"], client_id: CLI, redirect_uri: redirect_uri, resource: nil)
      assert_response :ok, redirect_uri
    end
  end

  test "a redirect that is not the loopback callback shows the error page and never redirects" do
    [ "https://cli.example.com/callback", "http://cli.example.com:53682/callback",
      "http://127.0.0.1.cli.example.com:53682/callback", "https://127.0.0.1:53682/callback",
      "http://127.0.0.1:53682/other", "http://127.0.0.1:53682/callback?next=1", "http://127.0.0.1:53682/callback#x",
      "http://user@127.0.0.1:53682/callback", "http://10.100.0.3:53682/callback", "127.0.0.1:53682/callback", "" ]
      .each do |redirect_uri|
      get oauth_authorize_url(authorize_params(CLI, redirect_uri: redirect_uri, scope: LOGIN_SCOPE, resource: nil)),
          headers: inertia_headers

      assert_error_page redirect_uri
    end
    get oauth_authorize_url(authorize_params(CLI, redirect_uri: nil, scope: LOGIN_SCOPE, resource: nil)),
        headers: inertia_headers
    assert_error_page "no redirect_uri"
    assert_equal 0, Oauth::PendingRequest.count
  end

  test "the code works only with the redirect URI, port and verifier that the request gave" do
    code = cli_code(redirect_uri: CLI_REDIRECT)

    exchange(code, client_id: CLI, redirect_uri: "http://127.0.0.1:53683/callback", resource: nil)
    assert_oauth_error "invalid_grant"

    code = cli_code(redirect_uri: CLI_REDIRECT)
    exchange(code, client_id: CLI, redirect_uri: CLI_REDIRECT, resource: nil, verifier: "x" * 43)
    assert_oauth_error "invalid_grant"
  end

  test "the approval page refuses a request with no PKCE, and sends the error only to the loopback redirect" do
    get oauth_authorize_url(authorize_params(CLI, redirect_uri: CLI_REDIRECT, scope: LOGIN_SCOPE, resource: nil,
                                                  code_challenge: nil))

    assert_response :redirect
    assert response.location.start_with?("#{CLI_REDIRECT}?")
    assert_equal "invalid_request", URI.decode_www_form(URI(response.location).query).to_h["error"]
  end

  test "a token of solidtrace-cli has only the scopes approved, in the Organization picked" do
    beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: beta, user: @user, role: :admin)

    tokens = cli_tokens(scope: "sessions:write", organization: beta)

    assert_equal "sessions:write", tokens["scope"]
    answer = introspect(tokens["access_token"])
    assert_equal [ true, "sessions:write", FACTORY, CLI, "ada@example.com" ],
                 answer.values_at("active", "scope", "aud", "client_id", "username")
    assert_equal [ [ "beta", "admin" ] ], answer["organizations"].map { |org| org.values_at("slug", "role") }
  end

  test "the token endpoint answers only the scopes of the token's resource" do
    tokens = cli_tokens(scope: "mcp factory", resource: FACTORY)

    assert_equal "factory", tokens["scope"]
    assert_equal "factory", introspect(tokens["access_token"])["scope"]
    refresh tokens["refresh_token"], resource: RESOURCE
    assert_response :ok
    assert_equal "mcp", response.parsed_body["scope"]
  end

  test "solidtrace-cli cannot use the code or the refresh token of another client, nor another client its own" do
    other = registered_client(redirect_uris: [ CLI_REDIRECT ])
    props = approval_page(other, redirect_uri: CLI_REDIRECT, scope: LOGIN_SCOPE, resource: nil)
    approve(props["request"], @acme)
    exchange(redirect_query["code"], client_id: CLI, redirect_uri: CLI_REDIRECT, resource: nil)
    assert_oauth_error "invalid_grant"

    tokens = cli_tokens
    refresh tokens["refresh_token"], client_id: other
    assert_oauth_error "invalid_grant"
    exchange(cli_code, client_id: other, redirect_uri: CLI_REDIRECT, resource: nil)
    assert_oauth_error "invalid_grant"
  end

  test "solidtrace-cli is public: a client secret gets invalid_client" do
    code = cli_code
    exchange(code, client_id: CLI, redirect_uri: CLI_REDIRECT, resource: nil,
                   headers: { "Authorization" => "Basic #{Base64.strict_encode64("#{CLI}:secret")}" })

    assert_oauth_error "invalid_client", status: :unauthorized
  end

  # Its client_id

  test "a registration cannot take the solidtrace-cli client_id or its checked name" do
    register(client_id: CLI, client_name: "SolidTrace CLI", redirect_uris: [ CLI_REDIRECT ])
    assert_response :created
    registered = response.parsed_body["client_id"]
    assert_not_equal CLI, registered

    client = approval_page(registered, redirect_uri: CLI_REDIRECT, scope: LOGIN_SCOPE, resource: nil)["client"]
    assert_equal [ "SolidTrace CLI", false, "Not checked: the client gave this name" ],
                 client.values_at("name", "name_checked", "name_note")
    assert_equal FIRST_PARTY_NOTE, cli_approval_page.dig("client", "name_note")
  end

  test "only the built-in client can have the solidtrace-cli client_id, and it can have no other" do
    Oauth::Client.first_party
    %w[dcr cimd].each do |kind|
      client = Oauth::Client.new(kind: kind, client_id: CLI, name: "SolidTrace CLI", redirect_uris: [ CLI_REDIRECT ])
      assert_not client.valid?, kind
      assert_includes client.errors[:client_id], "is solidtrace-cli for the built-in client, and only for it"
    end
    assert_not Oauth::Client.new(kind: "first_party", client_id: "other-cli", name: "CLI").valid?
  end

  private

  def cli_code(redirect_uri: CLI_REDIRECT, scope: LOGIN_SCOPE, organization: @acme, resource: nil)
    approve(cli_approval_page(redirect_uri: redirect_uri, scope: scope, resource: resource)["request"], organization)
    assert_response :conflict
    redirect_query["code"]
  end

  def cli_tokens(scope: LOGIN_SCOPE, organization: @acme, resource: nil)
    exchange(cli_code(scope: scope, organization: organization, resource: resource), client_id: CLI,
                                                                                         redirect_uri: CLI_REDIRECT,
                                                                                         resource: resource)
    assert_response :ok
    response.parsed_body
  end

  def refresh(refresh_token, client_id: CLI, **params)
    post oauth_token_url, params: { grant_type: "refresh_token", refresh_token: refresh_token, client_id: client_id,
                                    **params }
  end

  def introspect(token)
    @introspection_caller ||= ApiToken.issue!(user: create(:user), name: "Service").last
    post oauth_introspect_url, params: { token: token }, headers: { "Authorization" => "Bearer #{@introspection_caller}" }
    assert_response :ok
    response.parsed_body
  end

  def assert_error_page(message)
    assert_response :bad_request, message
    assert_equal "Oauth/Error", response.parsed_body["component"], message
    assert_nil response.headers["Location"], message
  end

  def assert_oauth_error(error, status: :bad_request)
    assert_response status
    assert_equal error, response.parsed_body["error"]
  end

  def cli_approval_page(redirect_uri: CLI_REDIRECT, scope: LOGIN_SCOPE, resource: nil)
    approval_page(CLI, redirect_uri: redirect_uri, scope: scope, resource: resource)
  end
end
