# frozen_string_literal: true

require "test_helper"
require "request_log"

# Sign-in through the browser, as an MCP client does it: the client
# registers (DCR), opens the approval page, the user signs in and approves
# for one Organization, the client exchanges the code with PKCE and calls
# POST /mcp with the access token.
class OauthBrowserSignInTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include TwoFactorTestHelper

  ISSUER = PasskeyTestHelper::CONSOLE_URL
  RESOURCE = "#{ISSUER}/mcp".freeze
  REDIRECT_URI = "http://localhost:4567/callback"
  # RFC 7636 Appendix B.
  VERIFIER = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
  CHALLENGE = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:organization_user, organization: @beta, user: @user, role: :member)
    create(:dashboard, organization: @acme, name: "Shop errors")
    create(:dashboard, organization: @beta, name: "Blog errors")
    open_console_at
  end

  # Registration

  test "registration makes a public client with the stored metadata, open to any origin" do
    register(client_name: "Claude Code", redirect_uris: [ REDIRECT_URI, "https://client.example.com/cb" ])

    assert_response :created
    body = response.parsed_body
    assert body["client_id"].present?
    assert_in_delta Time.current.to_i, body["client_id_issued_at"], 5
    assert_equal "Claude Code", body["client_name"]
    assert_equal [ REDIRECT_URI, "https://client.example.com/cb" ], body["redirect_uris"]
    assert_equal "none", body["token_endpoint_auth_method"]
    assert_equal %w[authorization_code refresh_token], body["grant_types"]
    assert_equal "*", response.headers["Access-Control-Allow-Origin"]
  end

  test "a registration with no client_name is an Unnamed client" do
    register(redirect_uris: [ REDIRECT_URI ])

    assert_response :created
    assert_equal "Unnamed client", response.parsed_body["client_name"]
  end

  test "registration refuses redirect URIs that are not HTTPS or loopback HTTP" do
    [ "http://client.example.com/cb", "myapp://callback", "javascript:alert(1)", "https://client.example.com/cb#x" ].each do |uri|
      register(redirect_uris: [ uri ])

      assert_response :bad_request, uri
      assert_equal "invalid_redirect_uri", response.parsed_body["error"], uri
      assert response.parsed_body["error_description"].present?
    end
    assert_equal 0, Oauth::Client.count
  end

  test "registration is limited to 10 in 3 minutes for each IP address" do
    store = ActiveSupport::Cache::MemoryStore.new
    Oauth::RegistrationsController.cache_store.stub(:increment, ->(*args, **options) { store.increment(*args, **options) }) do
      10.times do
        register(redirect_uris: [ REDIRECT_URI ])
        assert_response :created
      end
      register(redirect_uris: [ REDIRECT_URI ])
      assert_response :too_many_requests

      register(redirect_uris: [ REDIRECT_URI ], headers: { "REMOTE_ADDR" => "198.51.100.92" })
      assert_response :created
    end
  end

  # Sign-in before approval

  test "with no Session the approval page sends the browser only to /login, and the password and second step come back" do
    freeze_time
    secret = with_authenticator_app
    client_id = registered_client

    get oauth_authorize_url(authorize_params(client_id))
    assert_redirected_to login_url

    post login_url, params: { email: @user.email, password: "password" }
    assert_redirected_to second_step_url
    post second_step_url, params: { code: code_for(secret) }
    assert_match %r{\A#{Regexp.escape(ISSUER)}/oauth/authorize\?request=[\w-]+\z}, response.location

    get response.location, headers: inertia_headers
    assert_equal "Oauth/Authorize", response.parsed_body["component"]
  end

  test "Passkey sign-in comes back to the approval page" do
    sign_in_as @user
    add_passkey
    delete session_url
    client_id = registered_client

    get oauth_authorize_url(authorize_params(client_id))
    assert_redirected_to login_url
    sign_in_with_passkey

    assert_match %r{\A#{Regexp.escape(ISSUER)}/oauth/authorize\?request=[\w-]+\z}, response.location
    get response.location, headers: inertia_headers
    assert_equal "Oauth/Authorize", response.parsed_body["component"]
  end

  test "an unknown client or an unregistered redirect URI shows the error page and never redirects" do
    get oauth_authorize_url(authorize_params("nobody"))
    assert_response :bad_request
    assert_nil response.location

    client_id = registered_client
    get oauth_authorize_url(authorize_params(client_id, redirect_uri: "https://evil.example.com/cb"))
    assert_response :bad_request
    assert_nil response.location
  end

  # The approval page

  test "with a Session the page shows the unchecked name, the redirect host, the loopback warning, the Organizations and the Scope" do
    sign_in_as @user
    get dashboards_url(org_slug: "beta"), headers: inertia_headers # Beta becomes the last used Organization.
    client_id = registered_client

    props = approval_page(client_id)

    assert_equal "Claude Code", props.dig("client", "name")
    assert_equal "Not checked: the client gave this name", props.dig("client", "name_note")
    assert_equal "localhost", props.dig("client", "redirect_host")
    assert_equal "This program runs on a computer. Approve only if you started it.", props["loopback_warning"]
    assert_equal [ "Beta", "Acme" ], props["organizations"].map { |organization| organization["name"] }
    assert_equal [ "Use the MCP server as you, in Beta: read its Logs and Issues, and change its Dashboards, " \
                   "Monitors, Issues and Projects as your role allows." ], props["organizations"].first["scopes"]
    assert_equal "You can revoke this in Settings → User.", props["revoke_hint"]
  end

  test "a client with an HTTPS redirect URI gets no loopback warning" do
    sign_in_as @user
    client_id = registered_client(redirect_uris: [ "https://client.example.com/cb" ])

    props = approval_page(client_id, redirect_uri: "https://client.example.com/cb")

    assert_equal "client.example.com", props.dig("client", "redirect_host")
    assert_nil props["loopback_warning"]
  end

  test "three requests with a 512-character state each sign in and approve with no CookieOverflow" do
    client_id = registered_client
    states = %w[a b c].map { |letter| letter * 512 }
    states.each do |state|
      get oauth_authorize_url(authorize_params(client_id, state: state))
      assert_redirected_to login_url
    end

    sign_in_as @user
    assert_match %r{/oauth/authorize\?request=[\w-]+\z}, response.location
    get response.location, headers: inertia_headers
    approve(response.parsed_body.dig("props", "request"), @acme)
    assert_equal states.last, redirect_query["state"]

    keys = states.map { |state| approval_page(client_id, state: state)["request"] }
    keys.zip(states).each do |key, state|
      approve(key, @acme)
      assert_response :conflict
      assert_equal state, redirect_query["state"]
    end
  end

  test "the session keeps at most 3 requests: a fourth drops the oldest" do
    sign_in_as @user
    client_id = registered_client
    keys = 4.times.map { |n| approval_page(client_id, state: "s#{n}")["request"] }

    approve(keys.first, @acme)
    assert_response :bad_request
    assert_nil response.headers["X-Inertia-Location"]

    approve(keys.last, @acme)
    assert_response :conflict
  end

  test "a request older than 10 minutes is refused, and the next request deletes it" do
    client_id = registered_client
    get oauth_authorize_url(authorize_params(client_id))
    assert_redirected_to login_url

    travel 11.minutes
    sign_in_as @user
    assert_match %r{/oauth/authorize\?request=[\w-]+\z}, response.location
    get response.location, headers: inertia_headers
    assert_response :bad_request
    assert_equal "Oauth/Error", response.parsed_body["component"]

    # Another browser, with no cookie, starts a request.
    send_requests_to(ISSUER)
    get oauth_authorize_url(authorize_params(client_id))
    assert_redirected_to login_url
    assert_equal 1, Oauth::PendingRequest.count
  end

  test "a request id that is not in this browser's session is refused" do
    sign_in_as @user
    client_id = registered_client
    key = approval_page(client_id)["request"]

    other = open_session
    other.host! URI(ISSUER).host
    other.https!
    other.post login_url, params: { email: @user.email, password: "password" }
    other.post oauth_authorize_url, params: { request: key, organization_id: @acme.id, decision: "approve" }

    assert_equal 400, other.response.status
    assert_nil other.response.location
    assert_equal 0, Oauth::Grant.count
  end

  # Approve, the code and the token

  test "Approve answers an Inertia request with 409 and X-Inertia-Location to the redirect URI with code, state and iss" do
    sign_in_as @user
    client_id = registered_client
    key = approval_page(client_id, state: "xyz")["request"]

    approve(key, @acme)

    assert_response :conflict
    location = URI(response.headers["X-Inertia-Location"])
    assert_equal "http://localhost:4567/callback", "#{location.scheme}://#{location.host}:#{location.port}#{location.path}"
    query = redirect_query
    assert_match(/\A[A-Za-z0-9_-]{43}\z/, query["code"]) # 32 random bytes, base64url.
    assert_equal "xyz", query["state"]
    assert_equal ISSUER, query["iss"]
    assert_equal [ RESOURCE ], Oauth::Grant.sole.resources
  end

  test "Approve answers a plain browser request with a 302 to the same URL" do
    sign_in_as @user
    client_id = registered_client
    key = approval_page(client_id, state: "xyz")["request"]

    post oauth_authorize_url, params: { request: key, organization_id: @acme.id, decision: "approve" }

    assert_response :found
    location = URI(response.location)
    assert_equal "/callback", location.path
    query = URI.decode_www_form(location.query).to_h
    assert_equal %w[code iss state], query.keys.sort
  end

  test "a code is kept only as its SHA-256 digest and lasts 60 seconds" do
    code = approved_code
    assert Oauth::Code.exists?(code_digest: Digest::SHA256.hexdigest(code))
    assert_not Oauth::Code.where("code_digest = :code OR code_challenge = :code", code: code).exists?

    travel 61.seconds
    exchange(code)
    assert_response :bad_request
    assert_equal "invalid_grant", response.parsed_body["error"]
  end

  test "the code exchange with PKCE answers an access token and a refresh token, open to any origin" do
    exchange(approved_code)

    assert_response :ok
    body = response.parsed_body
    assert_match(/\Asto_[A-Za-z0-9_-]{43}\z/, body["access_token"])
    assert_match(/\Astr_[A-Za-z0-9_-]{43}\z/, body["refresh_token"])
    assert_equal "Bearer", body["token_type"]
    assert_equal 3600, body["expires_in"]
    assert_equal "mcp", body["scope"]
    assert_equal "*", response.headers["Access-Control-Allow-Origin"]
    assert_equal "no-store", response.headers["Cache-Control"]
  end

  test "the code exchange refuses a wrong code_verifier and a code used twice" do
    code = approved_code
    exchange(code, verifier: "#{VERIFIER}x")
    assert_equal "invalid_grant", response.parsed_body["error"]

    exchange(code)
    assert_response :ok
    exchange(code)
    assert_equal "invalid_grant", response.parsed_body["error"]
  end

  test "a code used a second time revokes its Grant, so the tokens of the first exchange stop" do
    code = approved_code
    exchange(code)
    token = response.parsed_body["access_token"]
    mcp "tools/call", token: token, name: "whoami", arguments: {}
    assert_response :ok

    exchange(code)
    assert_equal "invalid_grant", response.parsed_body["error"]

    mcp "tools/call", token: token, name: "whoami", arguments: {}
    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"], 'error="invalid_token"'
  end

  test "the code exchange refuses a redirect_uri that is not the authorization request's, or none" do
    code = approved_code

    [ "http://localhost:4567/other", "https://client.example.com/cb", nil ].each do |redirect_uri|
      exchange(code, redirect_uri: redirect_uri)
      assert_response :bad_request, redirect_uri.inspect
      assert_equal "invalid_grant", response.parsed_body["error"], redirect_uri.inspect
      assert_nil response.parsed_body["access_token"]
    end
  end

  test "a code works only for the client that got it" do
    code = approved_code
    other_client_id = registered_client

    exchange(code, client_id: other_client_id)

    assert_response :bad_request
    assert_equal "invalid_grant", response.parsed_body["error"]
    assert_nil response.parsed_body["access_token"]
  end

  test "a user who leaves the Organization gets the 401 at /mcp, and an unused code is invalid_grant" do
    token = access_token(organization: @acme)
    code = approved_code(organization: @acme)
    # With no callback, so the live-Grant check at use is what refuses.
    OrganizationUser.where(user: @user, organization: @acme).delete_all

    mcp "tools/call", token: token, name: "whoami", arguments: {}
    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"], 'error="invalid_token"'

    exchange(code)
    assert_equal "invalid_grant", response.parsed_body["error"]
  end

  test "a Grant whose resources are not under the current issuer is not live" do
    code = approved_code
    InstanceSetting.current.update!(console_url: "https://new-console.example.com")

    exchange(code)
    assert_equal "invalid_grant", response.parsed_body["error"]

    InstanceSetting.current.update!(console_url: ISSUER)
    token = access_token(organization: @acme)
    Oauth::Grant.update_all(resources: [])
    mcp "tools/call", token: token, name: "whoami", arguments: {}
    assert_response :unauthorized
  end

  test "Approve refuses an Organization that the user is not a member of" do
    other = create(:organization, name: "Other", slug: "other")
    sign_in_as @user
    client_id = registered_client
    key = approval_page(client_id)["request"]

    approve(key, other)

    assert_response :not_found
    assert_nil response.headers["X-Inertia-Location"]
    assert_nil response.location
    assert_equal 0, Oauth::Grant.count
    assert_equal 0, Oauth::Code.count
  end

  # POST /mcp with the access token

  test "whoami with the access token answers only the picked Organization" do
    token = access_token(organization: @beta)

    mcp "tools/call", token: token, name: "whoami", arguments: {}

    assert_response :ok
    answer = JSON.parse(response.parsed_body.dig("result", "content", 0, "text").split("\nConsole: ").first)
    assert_equal [ { "slug" => "beta", "name" => "Beta", "role" => "member" } ], answer["organizations"]
  end

  test "a tool call that names another Organization is a tool error, and links use the picked Organization" do
    token = access_token(organization: @acme)

    mcp "tools/call", token: token, name: "list_dashboards", arguments: { organization: "beta" }
    result = response.parsed_body["result"]
    assert_equal true, result["isError"]
    assert_equal "This sign-in is for acme. Sign in again to use another Organization.", result.dig("content", 0, "text")

    mcp "tools/call", token: token, name: "list_dashboards", arguments: {}
    text = response.parsed_body.dig("result", "content", 0, "text")
    assert_includes text, "Shop errors"
    assert_not_includes text, "Blog errors"
    assert_includes text, "\nConsole: #{ISSUER}/acme/dashboards"
  end

  test "an Instance admin's OAuth token gets no instance admin tools" do
    @user.update!(instance_admin: true)
    token = access_token(organization: @acme)
    instance_tools = %w[get_instance_settings update_instance_settings get_email_settings update_email_settings send_test_email]

    mcp "tools/list", token: token
    names = response.parsed_body.dig("result", "tools").map { |tool| tool["name"] }
    assert_includes names, "whoami"
    assert_empty names & instance_tools

    mcp "tools/call", token: token, name: "get_instance_settings", arguments: {}
    assert_match(/Tool not found: get_instance_settings/, response.parsed_body["error"].to_json)

    # An API token keeps them.
    _api_token, secret = ApiToken.issue!(user: @user, name: "Laptop")
    mcp "tools/list", token: secret
    assert_equal instance_tools.sort, (response.parsed_body.dig("result", "tools").map { |tool| tool["name"] } & instance_tools).sort
  end

  test "an access token gets the 401 with invalid_token after 1 hour" do
    token = access_token(organization: @acme)
    travel 59.minutes
    mcp "tools/call", token: token, name: "whoami", arguments: {}
    assert_response :ok

    travel 2.minutes
    mcp "tools/call", token: token, name: "whoami", arguments: {}
    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"], 'error="invalid_token"'
  end

  test "an access token whose resource is not <issuer>/mcp is refused at /mcp" do
    token = access_token(organization: @acme)
    Oauth::Token.where(kind: "access").update_all(resource: "#{ISSUER}/other")

    mcp "tools/call", token: token, name: "whoami", arguments: {}

    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"], 'error="invalid_token"'
  end

  test "the Grant's last used time is written at most once a minute" do
    freeze_time
    token = access_token(organization: @acme)
    mcp "tools/call", token: token, name: "whoami", arguments: {}
    first = Oauth::Grant.sole.last_used_at
    assert_equal Time.current, first

    travel 30.seconds
    mcp "tools/call", token: token, name: "whoami", arguments: {}
    assert_equal first, Oauth::Grant.sole.last_used_at

    travel 31.seconds
    mcp "tools/call", token: token, name: "whoami", arguments: {}
    assert_equal Time.current, Oauth::Grant.sole.last_used_at
  end

  # Secrets out of the logs

  test "an Approve and a token request write no code, verifier or token to the request log" do
    sign_in_as @user
    client_id = registered_client
    key = approval_page(client_id)["request"]

    logged = log_of do
      post oauth_authorize_url, params: { request: key, organization_id: @acme.id, decision: "approve" }
    end
    code = URI.decode_www_form(URI(response.location).query).to_h["code"]
    logged += log_of { exchange(code, client_id: client_id) }
    tokens = response.parsed_body.values_at("access_token", "refresh_token")

    assert_response :ok
    [ code, VERIFIER, *tokens ].each { |secret| assert_not_includes logged, secret }
    assert_includes logged, "[FILTERED]"
  end

  test "the filters cover code and verifier" do
    filter = ActiveSupport::ParameterFilter.new(Rails.application.config.filter_parameters)
    filtered = filter.filter("code" => "1", "code_verifier" => "2", "code_challenge" => "3", "refresh_token" => "4", "state" => "5")

    assert_equal({ "code" => "[FILTERED]", "code_verifier" => "[FILTERED]", "code_challenge" => "[FILTERED]",
                   "refresh_token" => "[FILTERED]", "state" => "5" }, filtered)
  end

  private

  def register(headers: {}, **metadata)
    post oauth_register_url, params: metadata.to_json, headers: { "Content-Type" => "application/json" }.merge(headers)
  end

  def registered_client(redirect_uris: [ REDIRECT_URI ])
    register(client_name: "Claude Code", redirect_uris: redirect_uris)
    assert_response :created
    response.parsed_body["client_id"]
  end

  def authorize_params(client_id, redirect_uri: REDIRECT_URI, state: "state-1")
    { response_type: "code", client_id: client_id, redirect_uri: redirect_uri, code_challenge: CHALLENGE,
      code_challenge_method: "S256", scope: "mcp", resource: RESOURCE, state: state }
  end

  # The approval page's props, for a signed-in user.
  def approval_page(client_id, **options)
    get oauth_authorize_url(authorize_params(client_id, **options)), headers: inertia_headers
    assert_response :ok
    assert_equal "Oauth/Authorize", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def approve(key, organization)
    post oauth_authorize_url, params: { request: key, organization_id: organization.id, decision: "approve" },
                              headers: inertia_headers
  end

  # The query of the URL that the last Approve sent the browser to.
  def redirect_query
    URI.decode_www_form(URI(response.headers["X-Inertia-Location"]).query).to_h
  end

  # A code from a signed-in Approve for organization; @client_id is its client.
  def approved_code(organization: @acme)
    sign_in_as @user
    @client_id = registered_client
    approve(approval_page(@client_id)["request"], organization)
    assert_response :conflict
    redirect_query["code"]
  end

  # A redirect_uri of nil is not sent.
  def exchange(code, verifier: VERIFIER, client_id: @client_id, redirect_uri: REDIRECT_URI)
    post oauth_token_url, params: { grant_type: "authorization_code", code: code, code_verifier: verifier,
                                    client_id: client_id, redirect_uri: redirect_uri, resource: RESOURCE }.compact
  end

  def access_token(organization:)
    exchange(approved_code(organization: organization))
    assert_response :ok
    response.parsed_body["access_token"]
  end

  def mcp(method, token:, **params)
    headers = { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json",
                "Authorization" => "Bearer #{token}" }
    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: method, params: params }.to_json, headers: headers
  end

  # What the request log reads from the requests in the block: each
  # request's params and path as RequestLog writes them, and each
  # redirect's location as Rails logs it.
  def log_of
    lines = []
    record = ->(name, _start, _finish, _id, payload) do
      lines << if name == "redirect_to.action_controller"
        payload[:location].to_s
      else
        [ payload[:path], RequestLog.request(Struct.new(:payload).new(payload)).to_json ].join(" ")
      end
    end
    ActiveSupport::Notifications.subscribed(record, /\A(process_action|redirect_to)\.action_controller\z/) { yield }
    lines.join("\n")
  end
end
