# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

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
  PUBLISHED = "https://cli.example.com/oauth/client.json"
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

  # The device flow

  test "solidtrace-cli signs in with a device code: the device page shows it as first-party, with its scopes" do
    answer = cli_device_code
    assert_equal %w[device_code expires_in interval user_code verification_uri], answer.keys.sort
    assert_equal "#{ISSUER}/device", answer["verification_uri"]

    props = device_approval_page(answer["user_code"])
    assert_equal({ "name" => "SolidTrace CLI", "name_note" => FIRST_PARTY_NOTE }, props["client"])
    assert_equal [ SESSIONS_WRITE_TEXT, FACTORY_TEXT ], props["organizations"].sole["scopes"]
    assert_equal PROGRAM_ADDRESS, props["asked_from"]

    decide props["request"], "approve"
    assert_equal "Oauth/Device", response.parsed_body["component"]
    poll answer["device_code"]
    assert_response :ok
    tokens = response.parsed_body
    assert_equal LOGIN_SCOPE, tokens["scope"]
    introspection = introspect(tokens["access_token"])
    assert_equal [ true, LOGIN_SCOPE, FACTORY, CLI ], introspection.values_at("active", "scope", "aud", "client_id")
    assert_equal [ "acme" ], introspection["organizations"].map { |org| org["slug"] }
    assert_equal "device", Oauth::Grant.sole.approved_via

    refresh tokens["refresh_token"]
    assert_response :ok
  end

  test "polling for solidtrace-cli answers authorization_pending, then access_denied after Deny" do
    answer = cli_device_code
    poll answer["device_code"]
    assert_oauth_error "authorization_pending"

    decide device_approval_page(answer["user_code"])["request"], "deny"
    travel 6.seconds
    poll answer["device_code"]
    assert_oauth_error "access_denied"
    assert_equal 0, Oauth::Grant.count
  end

  test "solidtrace-cli asking for a scope that is not for OAuth gets invalid_scope" do
    ask_for_device_code(scope: "sessions:write sessions:read")

    assert_oauth_error "invalid_scope"
  end

  test "a registered (DCR) client still cannot use the device flow, also with the name SolidTrace CLI" do
    register(client_name: "SolidTrace CLI", redirect_uris: [ CLI_REDIRECT ])
    registered = response.parsed_body["client_id"]
    register(client_name: "SolidTrace CLI", redirect_uris: [ CLI_REDIRECT ], grant_types: [ DEVICE_GRANT ])
    assert_response :bad_request

    ask_for_device_code(client_id: registered)

    assert_oauth_error "unauthorized_client"
    assert_equal 0, Oauth::Code.count
  end

  test "a device code of solidtrace-cli works for no other client, and no other's for solidtrace-cli" do
    answer = cli_device_code
    decide device_approval_page(answer["user_code"])["request"], "approve"
    other = registered_client(redirect_uris: [ CLI_REDIRECT ])

    [ other, PUBLISHED, "SolidTrace-CLI", "solidtrace-cli " ].each do |client_id|
      poll answer["device_code"], client_id: client_id
      assert_oauth_error "invalid_grant"
    end

    published = Oauth::DeviceCode.issue!(client_id: PUBLISHED,
                                         scopes: Oauth::Scopes.requested("mcp"), resource: RESOURCE,
                                         ip_address: PROGRAM_ADDRESS)[1]
    poll published
    assert_oauth_error "invalid_grant"

    poll answer["device_code"]
    assert_response :ok
  end

  test "a device poll of solidtrace-cli with a client secret gets invalid_client" do
    answer = cli_device_code

    poll answer["device_code"], client_secret: "secret"

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

  test "the built-in client's row gets back its own redirect URI and grant types at each use" do
    Oauth::Client.first_party.update_columns(redirect_uris: [ "https://cli.example.com/callback" ], grant_types: [],
                                             token_endpoint_auth_method: "client_secret_basic")

    get oauth_authorize_url(authorize_params(CLI, redirect_uri: "https://cli.example.com/callback", scope: LOGIN_SCOPE,
                                                  resource: nil)), headers: inertia_headers
    assert_error_page "a redirect URI written into the row"
    assert_equal "SolidTrace CLI", cli_approval_page.dig("client", "name")
    cli_tokens
    assert_equal [ "authorization_code", "refresh_token", DEVICE_GRANT ], Oauth::Client.named(CLI).grant_types
  end

  test "the built-in client's row lists the code, refresh and device grants, so it has the device grant" do
    client = Oauth::Client.first_party
    assert_equal [ "authorization_code", "refresh_token", DEVICE_GRANT ], client.grant_types
    assert_equal [ "http://127.0.0.1/callback" ], client.redirect_uris
    assert_equal "none", client.token_endpoint_auth_method
    assert client.device_grant?
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

  test "a published description that names the solidtrace-cli client_id is refused, on both pages" do
    with_published_description(client_id: CLI, client_name: "SolidTrace CLI") do
      get oauth_authorize_url(authorize_params(PUBLISHED, redirect_uri: CLI_REDIRECT, scope: LOGIN_SCOPE, resource: nil)),
          headers: inertia_headers
      assert_error_page "approval page"
      assert_match "names another client_id", response.parsed_body.dig("props", "message")

      user_code = Oauth::DeviceCode.issue!(client_id: PUBLISHED, scopes: Oauth::Scopes.requested(LOGIN_SCOPE),
                                           resource: FACTORY, ip_address: PROGRAM_ADDRESS)[2]
      post device_url, params: { user_code: user_code }, headers: inertia_headers
      assert_error_page "device page"
    end
    assert_equal 0, Oauth::Grant.count
    assert_not Oauth::Client.exists?(kind: "cimd", name: "SolidTrace CLI")
    assert_not Oauth::Client.where.not(kind: "first_party").exists?(client_id: CLI)
  end

  test "a published description with the name SolidTrace CLI shows its host, not the first-party note" do
    with_published_description(client_name: "SolidTrace CLI") do
      client = approval_page(PUBLISHED, redirect_uri: CLI_REDIRECT, scope: LOGIN_SCOPE, resource: nil)["client"]

      assert_equal [ "SolidTrace CLI", true, "Checked: published at cli.example.com" ],
                   client.values_at("name", "name_checked", "name_note")
    end
  end

  # The limits

  test "the limits of solidtrace-cli count by IP address: other addresses cannot use up the count of a user" do
    tokens = cli_tokens
    with_limits(Oauth::TokensController) do
      60.times { |n| post oauth_token_url, params: { grant_type: "refresh_token", refresh_token: "str_x", client_id: CLI },
                                           headers: { "REMOTE_ADDR" => "203.0.113.#{n + 100}" } }
      60.times { |n| ask_for_device_code(ip: "203.0.113.#{n + 100}") }
      60.times { |n| post oauth_revoke_url, params: { token: "sto_x", client_id: CLI },
                                            headers: { "REMOTE_ADDR" => "203.0.113.#{n + 100}" } }

      refresh tokens["refresh_token"]
      assert_response :ok
      access = response.parsed_body["access_token"]
      cli_device_code
      post oauth_revoke_url, params: { token: access, client_id: CLI }
      assert_response :ok
      assert_not introspect(access)["active"]

      # One address still has its limit.
      60.times { ask_for_device_code(ip: "203.0.113.100") }
      assert_oauth_error "invalid_request", status: :too_many_requests
    end
  end

  # Signed-in clients

  test "Settings → User lists each solidtrace-cli sign-in, and Revoke stops it at once" do
    browser = cli_tokens
    answer = cli_device_code
    decide device_approval_page(answer["user_code"])["request"], "approve"
    poll answer["device_code"]
    device = response.parsed_body

    get user_settings_url, headers: inertia_headers
    rows = response.parsed_body.dig("props", "signed_in_clients")
    assert_equal [ [ "device", "Acme" ], [ "browser", "Acme" ] ],
                 rows.map { |row| [ row["approved_via"], row.dig("organization", "name") ] }
    assert_equal [ { "name" => "SolidTrace CLI", "name_checked" => true, "host" => nil, "first_party" => true } ],
                 rows.map { |row| row["client"] }.uniq

    delete signed_in_client_url(rows.last["id"]), headers: inertia_headers.merge("Referer" => user_settings_url)
    assert_redirected_to user_settings_url

    assert_not introspect(browser["access_token"])["active"]
    refresh browser["refresh_token"]
    assert_oauth_error "invalid_grant"
    assert introspect(device["access_token"])["active"]
  end

  private

  # Serves a published description at PUBLISHED from a public address.
  def with_published_description(**changes)
    resolver = Oauth::PublishedDescription.resolver
    Oauth::PublishedDescription.resolver = ->(_host) { [ "198.51.100.92" ] }
    document = { client_id: PUBLISHED, client_name: "Trace CLI", redirect_uris: [ "http://127.0.0.1/callback" ],
                 grant_types: [ "authorization_code", "refresh_token", DEVICE_GRANT ],
                 token_endpoint_auth_method: "none" }.merge(changes)
    stub_request(:get, PUBLISHED).to_return(status: 200, body: document.to_json,
                                            headers: { "Content-Type" => "application/json" })
    yield
  ensure
    Oauth::PublishedDescription.resolver = resolver
  end

  def cli_code(redirect_uri: CLI_REDIRECT, scope: LOGIN_SCOPE, organization: @acme, resource: nil)
    approve(cli_approval_page(redirect_uri: redirect_uri, scope: scope, resource: resource)["request"], organization)
    assert_response :conflict
    redirect_query["code"]
  end

  def cli_tokens(scope: LOGIN_SCOPE, organization: @acme, resource: nil)
    code = cli_code(scope: scope, organization: organization, resource: resource)
    exchange(code, client_id: CLI, redirect_uri: CLI_REDIRECT, resource: resource)
    assert_response :ok
    response.parsed_body
  end

  def refresh(refresh_token, client_id: CLI, **params)
    post oauth_token_url, params: { grant_type: "refresh_token", refresh_token: refresh_token, client_id: client_id,
                                    **params }
  end

  def ask_for_device_code(client_id: CLI, scope: LOGIN_SCOPE, ip: PROGRAM_ADDRESS)
    post oauth_device_authorization_url, params: { client_id: client_id, scope: scope }, headers: { "REMOTE_ADDR" => ip }
  end

  # Counts the rate limits of controller in memory (the test cache is a null store).
  def with_limits(controller, &block)
    store = ActiveSupport::Cache::MemoryStore.new
    controller.cache_store.stub(:increment, ->(*args, **options) { store.increment(*args, **options) }, &block)
  end

  def cli_device_code
    ask_for_device_code
    assert_response :ok
    response.parsed_body
  end

  # The approval props on the device page after the user types user_code.
  def device_approval_page(user_code)
    post device_url, params: { user_code: user_code }, headers: inertia_headers
    assert_response :redirect
    get response.location, headers: inertia_headers
    assert_response :ok
    assert_equal "Oauth/DeviceApproval", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def decide(key, decision, organization: @acme)
    post device_url, params: { request: key, organization_id: organization.id, decision: decision },
                     headers: inertia_headers
    assert_response :ok
  end

  def poll(device_code, client_id: CLI, **params)
    post oauth_token_url, params: { grant_type: DEVICE_GRANT, device_code: device_code, client_id: client_id, **params },
                          headers: { "REMOTE_ADDR" => PROGRAM_ADDRESS }
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
