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
      authorize_params(client_id).except(:redirect_uri),
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

  private

  def deny(key, ip: nil)
    headers = inertia_headers
    headers = headers.merge("REMOTE_ADDR" => ip) if ip
    post oauth_authorize_url, params: { request: key, organization_id: @acme.id, decision: "deny" }, headers: headers
  end
end
