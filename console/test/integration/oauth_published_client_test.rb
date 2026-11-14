# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# Sign-in through the browser for a client whose client_id is the HTTPS URL
# of its published description (a Client ID Metadata Document), as Claude
# Code uses. Console fetches the description only for a signed-in user,
# with SSRF guards and limits, keeps a copy, and the token endpoint uses
# that copy. WebMock serves the description; the host's addresses come from
# a stubbed resolver.
class OauthPublishedClientTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include TwoFactorTestHelper

  ISSUER = PasskeyTestHelper::CONSOLE_URL
  RESOURCE = "#{ISSUER}/mcp".freeze
  CLIENT_ID = "https://client.example.com/oauth/client.json"
  REDIRECT_URI = "http://localhost:4567/callback"
  PUBLIC_ADDRESS = "198.51.100.92"
  # RFC 7636 Appendix B.
  VERIFIER = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
  CHALLENGE = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:dashboard, organization: @acme, name: "Shop errors")
    open_console_at
    @addresses = { "client.example.com" => [ PUBLIC_ADDRESS ] }
    @resolver = Oauth::PublishedDescription.resolver
    Oauth::PublishedDescription.resolver = ->(host) { @addresses.fetch(host) { raise SocketError, "unknown host" } }
  end

  teardown do
    Oauth::PublishedDescription.resolver = @resolver
    Oauth::PublishedDescription.limit_store = nil
  end

  # Before sign-in: the form check only

  test "before sign-in a published client gets only the form check, and no request goes out" do
    get oauth_authorize_url(authorize_params)

    assert_redirected_to login_url
    assert_not_requested :any, /.*/
  end

  # After sign-in: the fetch and the checks

  test "after sign-in the page shows the published name, where it comes from, and the redirect host" do
    publish
    sign_in_as @user

    props = approval_page

    assert_equal "Claude Code", props.dig("client", "name")
    assert_equal "Checked: published at client.example.com", props.dig("client", "name_note")
    assert props.dig("client", "name_checked")
    assert_equal "localhost", props.dig("client", "redirect_host")
    assert_requested :get, CLIENT_ID, times: 1
  end

  private

  def description(**changes)
    { client_id: CLIENT_ID, client_name: "Claude Code", redirect_uris: [ REDIRECT_URI ],
      grant_types: %w[authorization_code refresh_token], token_endpoint_auth_method: "none" }.merge(changes).compact
  end

  # Serves the description at url, with headers.
  def publish(url = CLIENT_ID, headers: { "Cache-Control" => "max-age=3600" }, **changes)
    stub_request(:get, url).to_return(status: 200, body: description(client_id: url, **changes).to_json,
                                      headers: { "Content-Type" => "application/json" }.merge(headers))
  end

  def authorize_params(client_id: CLIENT_ID, redirect_uri: REDIRECT_URI, state: "state-1")
    { response_type: "code", client_id: client_id, redirect_uri: redirect_uri, code_challenge: CHALLENGE,
      code_challenge_method: "S256", scope: "mcp", resource: RESOURCE, state: state }
  end

  # The approval page's props, for a signed-in user.
  def approval_page(**options)
    get oauth_authorize_url(authorize_params(**options)), headers: inertia_headers
    assert_response :ok
    assert_equal "Oauth/Authorize", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  # The error page's message, for a signed-in user; it never redirects.
  def error_page(**options)
    get oauth_authorize_url(authorize_params(**options)), headers: inertia_headers
    assert_response :bad_request
    assert_nil response.location
    assert_nil response.headers["X-Inertia-Location"]
    assert_equal "Oauth/Error", response.parsed_body["component"]
    response.parsed_body.dig("props", "message")
  end
end
