# frozen_string_literal: true

require "test_helper"

# The refusals of sign-in through the browser: a bad client or redirect URI
# shows an error page and never sends the browser on; other bad parameters
# go back to the client with an OAuth error; a code exchange with any wrong
# value fails; and a confidential client must authenticate.
class OauthRefusalsTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include OauthTestHelper

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    open_console_at
  end

  # A bad client or redirect URI

  test "an unknown, missing or unregistered client or redirect URI shows the error page and never redirects, before and after sign-in" do
    client_id = registered_client
    bad = [
      authorize_params("nobody"),
      authorize_params(nil).except(:client_id),
      authorize_params(client_id, redirect_uri: "https://evil.example.com/callback"),
      authorize_params(client_id).except(:redirect_uri)
    ]

    [ false, true ].each do |signed_in|
      sign_in_as @user if signed_in
      bad.each do |query|
        get oauth_authorize_url(query), headers: inertia_headers
        assert_response :bad_request, query.inspect
        assert_equal "Oauth/Error", response.parsed_body["component"], query.inspect
        assert response.parsed_body.dig("props", "message").present?
        assert_nil response.location
        assert_nil response.headers["X-Inertia-Location"]

        get oauth_authorize_url(query)
        assert_response :bad_request, query.inspect
        assert_nil response.location
      end
    end
  end

  test "a registered loopback redirect URI matches any port and the three loopback hosts, and nothing else" do
    sign_in_as @user
    client_id = registered_client(redirect_uris: [ "http://localhost:1234/callback" ])

    [ "http://localhost:1234/callback", "http://127.0.0.1:5678/callback", "http://[::1]:9/callback",
      "http://localhost/callback", "http://LOCALHOST:80/callback" ].each do |uri|
      get oauth_authorize_url(authorize_params(client_id, redirect_uri: uri)), headers: inertia_headers
      assert_response :ok, uri
      assert_equal "Oauth/Authorize", response.parsed_body["component"], uri
    end

    [ "http://localhost:1234/other", "http://localhost:1234/callback/", "http://localhost:1234/Callback",
      "http://localhost:1234/callback?next=x", "http://localhost:1234/callback#x", "https://localhost:1234/callback",
      "https://127.0.0.1:1234/callback", "http://user@localhost:1234/callback", "http://localhost.example.com/callback",
      "http://example.com:1234/callback", "http://127.0.0.2:1234/callback", "myapp://localhost/callback" ].each do |uri|
      get oauth_authorize_url(authorize_params(client_id, redirect_uri: uri)), headers: inertia_headers
      assert_response :bad_request, uri
      assert_equal "Oauth/Error", response.parsed_body["component"], uri
      assert_nil response.location
    end
  end

  test "Approve sends the code to the loopback redirect URI that the request gave, with its port" do
    sign_in_as @user
    client_id = registered_client(redirect_uris: [ "http://localhost:1234/callback" ])
    key = approval_page(client_id, redirect_uri: "http://[::1]:9/callback")["request"]

    approve(key, @acme)

    assert_response :conflict
    assert_match %r{\Ahttp://\[::1\]:9/callback\?code=[\w-]+&state=state-1&iss=}, response.headers["X-Inertia-Location"]
  end

  test "an HTTPS redirect URI must match exactly" do
    sign_in_as @user
    client_id = registered_client(redirect_uris: [ "https://client.example.com/callback" ])
    approval_page(client_id, redirect_uri: "https://client.example.com/callback")

    [ "https://client.example.com/callback/", "http://client.example.com/callback", "https://client.example.com:8443/callback",
      "https://other.example.com/callback", "https://client.example.com/callback?x=1" ].each do |uri|
      get oauth_authorize_url(authorize_params(client_id, redirect_uri: uri))
      assert_response :bad_request, uri
      assert_nil response.location
    end
  end

  # Other bad parameters

  test "after sign-in, each bad parameter goes back to the client with the OAuth error, state and iss" do
    sign_in_as @user
    client_id = registered_client
    cases = {
      { response_type: nil } => "invalid_request",
      { response_type: "token" } => "unsupported_response_type",
      { code_challenge: nil } => "invalid_request",
      { code_challenge_method: "plain" } => "invalid_request",
      { code_challenge_method: nil } => "invalid_request",
      { code_challenge: "too-short" } => "invalid_request",
      { code_challenge: "#{CHALLENGE}x" } => "invalid_request",
      { scope: "mcp admin" } => "invalid_scope",
      { resource: "https://other.example.com/mcp" } => "invalid_target",
      { resource: "#{ISSUER}/api" } => "invalid_target",
      { state: "s" * 513 } => "invalid_request",
    }

    cases.each do |change, error|
      query = authorize_params(client_id).merge(change).compact
      get oauth_authorize_url(query)

      assert_response :redirect, change.inspect
      assert response.location.start_with?("#{REDIRECT_URI}?"), change.inspect
      answer = URI.decode_www_form(URI(response.location).query).to_h
      assert_equal error, answer["error"], change.inspect
      assert answer["error_description"].present?, change.inspect
      assert_equal query[:state], answer["state"], change.inspect
      assert_equal ISSUER, answer["iss"], change.inspect
      assert_nil answer["code"]
    end
    assert_equal 0, Oauth::Grant.count
  end

  test "the boundaries of the parameters: a 512-character state, no scope, and the resource in another case or with a slash" do
    sign_in_as @user
    client_id = registered_client

    [ { state: "s" * 512 }, { scope: nil }, { resource: nil }, { resource: "HTTPS://CONSOLE.example.com/mcp/" } ].each do |change|
      get oauth_authorize_url(authorize_params(client_id).merge(change).compact), headers: inertia_headers
      assert_response :ok, change.inspect
      assert_equal "Oauth/Authorize", response.parsed_body["component"], change.inspect
    end
  end

  test "before sign-in a bad parameter goes only to /login, and after sign-in back to the client with the error" do
    client_id = registered_client
    get oauth_authorize_url(authorize_params(client_id).merge(code_challenge_method: "plain"))
    assert_redirected_to login_url

    sign_in_as @user
    get response.location

    assert response.location.start_with?("#{REDIRECT_URI}?")
    assert_equal "invalid_request", URI.decode_www_form(URI(response.location).query).to_h["error"]
  end

  # Deny and the pending request

  test "Deny goes back to the client with access_denied, state and iss, and makes no Grant" do
    sign_in_as @user
    client_id = registered_client
    key = approval_page(client_id)["request"]

    post oauth_authorize_url, params: { request: key, organization_id: @acme.id, decision: "deny" }, headers: inertia_headers

    assert_response :conflict
    assert response.headers["X-Inertia-Location"].start_with?("#{REDIRECT_URI}?")
    answer = redirect_query
    assert_equal "access_denied", answer["error"]
    assert answer["error_description"].present?
    assert_equal "state-1", answer["state"]
    assert_equal ISSUER, answer["iss"]
    assert_nil answer["code"]
    assert_equal 0, Oauth::Grant.count
    assert_equal 0, Oauth::Code.count

    approve(key, @acme)
    assert_response :bad_request
    assert_equal "Oauth/Error", response.parsed_body["component"]
  end

  test "a POST with no request id, an unknown one, or one older than 10 minutes shows the error page" do
    sign_in_as @user
    client_id = registered_client
    key = approval_page(client_id)["request"]

    [ nil, "unknown" ].each do |id|
      post oauth_authorize_url, params: { request: id, organization_id: @acme.id, decision: "approve" }.compact,
                                headers: inertia_headers
      assert_response :bad_request, id.inspect
      assert_equal "Oauth/Error", response.parsed_body["component"]
      assert_nil response.headers["X-Inertia-Location"]
    end

    travel 10.minutes + 1.second
    approve(key, @acme)
    assert_response :bad_request
    assert_equal "Oauth/Error", response.parsed_body["component"]
    assert_nil response.headers["X-Inertia-Location"]
    assert_equal 0, Oauth::Grant.count
  end

  test "a request just under 10 minutes old still approves, and its id works only once" do
    sign_in_as @user
    client_id = registered_client
    key = approval_page(client_id)["request"]

    travel 9.minutes + 59.seconds
    approve(key, @acme)
    assert_response :conflict

    approve(key, @acme)
    assert_response :bad_request
    assert_equal "Oauth/Error", response.parsed_body["component"]
    assert_equal 1, Oauth::Grant.count
  end

  test "Approve with no Organization id is refused and makes no Grant" do
    sign_in_as @user
    key = approval_page(registered_client)["request"]

    post oauth_authorize_url, params: { request: key, decision: "approve" }, headers: inertia_headers

    assert_response :not_found
    assert_nil response.headers["X-Inertia-Location"]
    assert_equal 0, Oauth::Grant.count
  end

  test "a user in no Organization sees that they are in none, can only Deny, and Approve makes no Grant" do
    loner = create(:user, name: "Lone", email: "lone@example.com")
    sign_in_as loner
    client_id = registered_client

    props = approval_page(client_id)
    assert_equal [], props["organizations"]
    assert_match "in no Organization", props["no_organization"]

    approve(props["request"], @acme)
    assert_response :not_found
    assert_equal 0, Oauth::Grant.count

    post oauth_authorize_url, params: { request: props["request"], decision: "deny" }, headers: inertia_headers
    assert_response :conflict
    assert_equal "access_denied", redirect_query["error"]
    assert_equal 0, Oauth::Grant.count
  end

  test "a user in an Organization gets no no-Organization note" do
    sign_in_as @user
    assert_nil approval_page(registered_client)["no_organization"]
  end

  test "Approve and Deny are limited to 10 in 3 minutes for each user and for each IP address" do
    other = create(:user, name: "Bob", email: "bob@example.com")
    create(:organization_user, organization: @acme, user: other, role: :member)
    store = ActiveSupport::Cache::MemoryStore.new
    Oauth::AuthorizationsController.cache_store.stub(:increment, ->(*args, **options) { store.increment(*args, **options) }) do
      sign_in_as @user
      client_id = registered_client
      10.times do
        deny(approval_page(client_id)["request"])
        assert_response :conflict
      end

      deny(approval_page(client_id)["request"])
      assert_response :too_many_requests
      assert_equal "Oauth/Error", response.parsed_body["component"]
      assert_nil response.headers["X-Inertia-Location"]

      # The same user from another address.
      deny(approval_page(client_id)["request"], ip: "198.51.100.92")
      assert_response :too_many_requests

      # Another user from the same address.
      delete session_url
      sign_in_as other
      deny(approval_page(client_id)["request"])
      assert_response :too_many_requests

      # Another user from another address.
      deny(approval_page(client_id)["request"], ip: "10.100.0.3")
      assert_response :conflict
    end
  end

  # The code exchange

  test "the code exchange answers invalid_grant for an unknown or missing code, a wrong code_verifier, client_id or redirect_uri" do
    code = approved_code
    other_client_id = registered_client
    other_verifier = "A" * 43

    [ { code: "unknown" }, { code: nil }, { verifier: other_verifier }, { verifier: nil }, { client_id: other_client_id },
      { redirect_uri: "http://127.0.0.1:4567/callback" }, { redirect_uri: "http://localhost:4567/callback/" } ].each do |change|
      exchange(change.fetch(:code, code), **change.except(:code))
      assert_response :bad_request, change.inspect
      assert_equal "invalid_grant", response.parsed_body["error"], change.inspect
      assert response.parsed_body["error_description"].present?
      assert_nil response.parsed_body["access_token"]
    end
  end

  test "a code_verifier must have 43 to 128 characters, as RFC 7636 says" do
    { "v" * 42 => :bad_request, "v" * 43 => :ok, "v" * 128 => :ok, "v" * 129 => :bad_request,
      "#{'v' * 42}+" => :bad_request }.each do |verifier, status|
      code = approved_code(challenge: Oauth::Secret.s256(verifier))
      exchange(code, verifier: verifier)
      assert_response status, verifier
      assert_equal "invalid_grant", response.parsed_body["error"], verifier if status == :bad_request
    end
  end

  test "an expired code answers invalid_grant: a code lasts 60 seconds" do
    freeze_time
    code = approved_code
    travel 59.seconds
    exchange(code)
    assert_response :ok

    code = approved_code
    travel 60.seconds
    exchange(code)
    assert_response :bad_request
    assert_equal "invalid_grant", response.parsed_body["error"]
  end

  test "the code exchange takes no resource, or the code's resource in another case or with a slash" do
    [ nil, "HTTPS://CONSOLE.EXAMPLE.COM/mcp/" ].each do |resource|
      exchange(approved_code, resource: resource)
      assert_response :ok, resource.inspect
      assert_equal "mcp", response.parsed_body["scope"]
    end
  end

  test "a resource that is not in the Grant answers invalid_target, and one in the Grant that is not the code's answers invalid_grant" do
    logs = Oauth::Scopes::Scope.new(name: "logs", resource_path: "/logs", text: "Read the Logs of %{organization}.")
    find = ->(name) { name == "logs" ? logs : Oauth::Scopes::ALL.find { |scope| scope.name == name } }
    Oauth::Scopes.stub(:find, find) do
      sign_in_as @user
      @client_id = registered_client
      get oauth_authorize_url(authorize_params(@client_id).merge(scope: "mcp logs")), headers: inertia_headers
      assert_response :ok
      approve(response.parsed_body.dig("props", "request"), @acme)
      code = redirect_query["code"]

      [ "https://other.example.com/mcp", "#{ISSUER}/api", "not a URL" ].each do |resource|
        exchange(code, resource: resource)
        assert_response :bad_request, resource
        assert_equal "invalid_target", response.parsed_body["error"], resource
        assert response.parsed_body["error_description"].present?
      end

      exchange(code, resource: "#{ISSUER}/logs")
      assert_response :bad_request
      assert_equal "invalid_grant", response.parsed_body["error"]
    end
  end

  test "two parallel exchanges of one code give exactly one token answer" do
    code = approved_code
    params = { grant_type: "authorization_code", code: code, code_verifier: VERIFIER, client_id: @client_id,
               redirect_uri: REDIRECT_URI, resource: RESOURCE }
    browsers = Array.new(2) { open_session.tap { |browser| browser.host!(URI(ISSUER).host) && browser.https! } }
    start = Queue.new

    threads = browsers.map do |browser|
      Thread.new do
        start.pop
        browser.post "/oauth/token", params: params
        [ browser.response.status, browser.response.parsed_body ]
      end
    end
    2.times { start << true }
    answers = threads.map(&:value)

    assert_equal [ 200, 400 ], answers.map(&:first).sort
    assert_equal 1, answers.count { |_status, body| body["access_token"].present? }
    assert_equal [ "invalid_grant" ], answers.filter_map { |_status, body| body["error"] }
  end

  test "a token request with no grant_type answers invalid_request, and an unknown one unsupported_grant_type" do
    post oauth_token_url, params: { code: "x" }
    assert_response :bad_request
    assert_equal "invalid_request", response.parsed_body["error"]

    post oauth_token_url, params: { grant_type: "password" }
    assert_response :bad_request
    assert_equal "unsupported_grant_type", response.parsed_body["error"]
  end

  # Confidential clients

  test "a confidential client gets its client_secret once, with client_secret_expires_at 0" do
    %w[client_secret_basic client_secret_post].each do |method|
      register(client_name: "Server app", redirect_uris: [ "https://app.example.com/callback" ], token_endpoint_auth_method: method)
      assert_response :created, method
      body = response.parsed_body
      assert_equal method, body["token_endpoint_auth_method"]
      assert_match(/\A[\w-]{43}\z/, body["client_secret"])
      assert_equal 0, body["client_secret_expires_at"]
    end

    register(client_name: "CLI", redirect_uris: [ REDIRECT_URI ])
    assert_not response.parsed_body.key?("client_secret")
    assert_not response.parsed_body.key?("client_secret_expires_at")
  end

  test "a registration with another token_endpoint_auth_method, or that asks for the device grant, gets invalid_client_metadata" do
    [ { token_endpoint_auth_method: "private_key_jwt" }, { token_endpoint_auth_method: "client_secret_jwt" },
      { grant_types: [ "authorization_code", "urn:ietf:params:oauth:grant-type:device_code" ] },
      { grant_types: [ "urn:ietf:params:oauth:grant-type:device_code" ] }, { grant_types: "authorization_code" } ].each do |metadata|
      register(redirect_uris: [ REDIRECT_URI ], **metadata)
      assert_response :bad_request, metadata.inspect
      assert_equal "invalid_client_metadata", response.parsed_body["error"], metadata.inspect
      assert response.parsed_body["error_description"].present?
    end
    assert_equal 0, Oauth::Client.count
  end

  test "a confidential client authenticates with client_secret_basic or client_secret_post" do
    client_id, secret = confidential_client("client_secret_basic")
    exchange(confidential_code(client_id), client_id: nil, headers: basic(client_id, secret))
    assert_response :ok
    assert response.parsed_body["access_token"].start_with?("sto_")

    client_id, secret = confidential_client("client_secret_post")
    post oauth_token_url, params: token_params(confidential_code(client_id), client_id).merge(client_secret: secret)
    assert_response :ok
    assert response.parsed_body["access_token"].start_with?("sto_")
  end

  test "a wrong or missing client secret answers invalid_client, and the code still works for the client" do
    client_id, secret = confidential_client("client_secret_basic")
    code = confidential_code(client_id)

    [ basic(client_id, "#{secret}x"), basic(client_id, ""), basic("nobody", secret), {},
      { "Authorization" => "Basic not-base64!" }, { "Authorization" => "Bearer #{secret}" },
      # Parts that are not UTF-8, form-encoded or raw.
      raw_basic("%FF:x"), raw_basic("\xFF\xFE:x".b), raw_basic("#{client_id}:%FF") ].each do |headers|
      exchange(code, client_id: (client_id unless headers.key?("Authorization")), headers: headers)
      assert_response :unauthorized, headers.inspect
      assert_equal "invalid_client", response.parsed_body["error"], headers.inspect
      assert response.parsed_body["error_description"].present?
    end

    post oauth_token_url, params: token_params(code, client_id).merge(client_secret: "#{secret}x")
    assert_response :unauthorized
    assert_equal "invalid_client", response.parsed_body["error"]

    # Basic for one client with the client_id of another.
    exchange(code, client_id: registered_client, headers: basic(client_id, secret))
    assert_response :unauthorized
    assert_equal "invalid_client", response.parsed_body["error"]

    exchange(code, client_id: nil, headers: basic(client_id, secret))
    assert_response :ok
  end

  test "a client that sends its secret two ways answers invalid_request" do
    client_id, secret = confidential_client("client_secret_basic")
    post oauth_token_url, params: token_params(confidential_code(client_id), client_id).merge(client_secret: secret),
                          headers: basic(client_id, secret)

    assert_response :bad_request
    assert_equal "invalid_request", response.parsed_body["error"]
  end

  test "a public client that sends a client secret answers invalid_client" do
    code = approved_code
    exchange(code, client_id: nil, headers: basic(@client_id, "secret"))
    assert_response :unauthorized
    assert_equal "invalid_client", response.parsed_body["error"]

    post oauth_token_url, params: token_params(code, @client_id).merge(client_secret: "secret")
    assert_response :unauthorized
    assert_equal "invalid_client", response.parsed_body["error"]
  end

  # Limits and errors

  test "/oauth/token is limited to 60 a minute for each IP address and for each client_id" do
    store = ActiveSupport::Cache::MemoryStore.new
    Oauth::TokensController.cache_store.stub(:increment, ->(*args, **options) { store.increment(*args, **options) }) do
      60.times do
        exchange("unknown", client_id: "client-a")
        assert_response :unauthorized
      end

      exchange("unknown", client_id: "client-a")
      assert_response :too_many_requests
      assert_equal "invalid_request", response.parsed_body["error"]
      assert response.parsed_body["error_description"].present?

      # The same client_id from another address.
      exchange("unknown", client_id: "client-a", headers: { "REMOTE_ADDR" => "198.51.100.92" })
      assert_response :too_many_requests

      # The client_id in Basic counts as the client_id.
      exchange("unknown", client_id: nil, headers: basic("client-a", "x").merge("REMOTE_ADDR" => "198.51.100.92"))
      assert_response :too_many_requests

      # Another client_id from the same address.
      exchange("unknown", client_id: "client-b")
      assert_response :too_many_requests

      # Another client_id from another address.
      exchange("unknown", client_id: "client-b", headers: { "REMOTE_ADDR" => "10.100.0.3" })
      assert_response :unauthorized
    end
  end

  test "every OAuth JSON error has error and an error_description that says what to do" do
    answers = []
    record = -> { answers << [ request.path, response.status, response.parsed_body ] }

    post oauth_token_url
    record.call
    post oauth_token_url, params: { grant_type: "authorization_code", client_id: "nobody", code: "x" }
    record.call
    post oauth_token_url, params: { grant_type: "authorization_code", client_id: registered_client, code: "x" }
    record.call
    post oauth_token_url, params: { grant_type: "refresh_token[]" }
    record.call
    post oauth_register_url, params: "{not json", headers: { "Content-Type" => "application/json" }
    record.call
    post oauth_register_url, params: "[]", headers: { "Content-Type" => "application/json" }
    record.call
    register(redirect_uris: "https://app.example.com/callback")
    record.call
    register(redirect_uris: [ REDIRECT_URI ], client_name: "n" * 101)
    record.call
    register(redirect_uris: [ REDIRECT_URI ], client_name: 7)
    record.call

    answers.each do |path, status, body|
      assert_includes 400..401, status, [ path, body ].inspect
      assert body["error"].present?, [ path, body ].inspect
      assert body["error_description"].is_a?(String) && body["error_description"].length > 10, [ path, body ].inspect
    end
  end

  private

  def confidential_client(method)
    register(client_name: "Server app", redirect_uris: [ REDIRECT_URI ], token_endpoint_auth_method: method)
    assert_response :created
    response.parsed_body.values_at("client_id", "client_secret")
  end

  def confidential_code(client_id)
    sign_in_as @user
    approve(approval_page(client_id)["request"], @acme)
    assert_response :conflict
    redirect_query["code"]
  end

  def token_params(code, client_id)
    { grant_type: "authorization_code", code: code, code_verifier: VERIFIER, client_id: client_id,
      redirect_uri: REDIRECT_URI, resource: RESOURCE }
  end

  # client_secret_basic: the form-encoded client_id and secret (RFC 6749 §2.3.1).
  def basic(client_id, secret)
    pair = [ client_id, secret ].map { |value| URI.encode_www_form_component(value) }.join(":")
    { "Authorization" => "Basic #{Base64.strict_encode64(pair)}" }
  end

  # Authorization: Basic with pair as it is, not form-encoded.
  def raw_basic(pair)
    { "Authorization" => "Basic #{Base64.strict_encode64(pair)}" }
  end

  # A code from a signed-in Approve with this PKCE challenge.
  def approved_code(organization: @acme, challenge: CHALLENGE)
    return super(organization: organization) if challenge == CHALLENGE

    sign_in_as @user
    @client_id = registered_client
    get oauth_authorize_url(authorize_params(@client_id).merge(code_challenge: challenge)), headers: inertia_headers
    approve(response.parsed_body.dig("props", "request"), organization)
    assert_response :conflict
    redirect_query["code"]
  end

  def deny(key, ip: nil)
    headers = inertia_headers
    headers = headers.merge("REMOTE_ADDR" => ip) if ip
    post oauth_authorize_url, params: { request: key, organization_id: @acme.id, decision: "deny" }, headers: headers
  end
end
