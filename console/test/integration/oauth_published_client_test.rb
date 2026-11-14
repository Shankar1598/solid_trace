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
    Oauth::PublishedDescription.time_limit = Oauth::PublishedDescription::TIMEOUT
  end

  # Before sign-in: the form check only

  test "before sign-in a published client gets only the form check, and no request goes out" do
    get oauth_authorize_url(authorize_params)

    assert_redirected_to login_url
    assert_not_requested :any, /.*/
  end

  test "a client_id of a bad form shows the error page before sign-in, and no request goes out" do
    long = "https://client.example.com/#{'a' * (513 - 'https://client.example.com/'.length)}"
    [
      "http://client.example.com/oauth/client.json",
      "https://client.example.com",
      "https://client.example.com/",
      "https://user:secret@client.example.com/oauth/client.json",
      "https://client.example.com/oauth/client.json#x",
      "https://client.example.com/oauth/../client.json",
      "https://127.0.0.1/client.json",
      "https://10.0.0.5/client.json",
      "https://169.254.169.254/client.json",
      "https://[::1]/client.json",
      "https://[::ffff:127.0.0.1]/client.json",
      "https://0.0.0.0/client.json",
      long
    ].each do |client_id|
      get oauth_authorize_url(authorize_params(client_id: client_id))

      assert_response :bad_request, client_id
      assert_nil response.location, client_id
    end
    assert_not_requested :any, /.*/
  end

  test "a client_id of at most 512 characters, or at a public address, passes the form check" do
    at_limit = "https://client.example.com/#{'a' * (512 - 'https://client.example.com/'.length)}"
    [ at_limit, "https://#{PUBLIC_ADDRESS}/client.json", "https://client.example.com/client.json?v=1" ].each do |client_id|
      get oauth_authorize_url(authorize_params(client_id: client_id))

      assert_redirected_to login_url, client_id
    end
  end

  # After sign-in: the fetch and the checks

  test "a description that names another client_id, lacks the redirect URI, or is for a confidential client shows the error page" do
    sign_in_as @user
    [
      { client_id: "https://other.example.com/client.json" },
      { redirect_uris: [ "https://client.example.com/callback" ] },
      { redirect_uris: nil },
      { redirect_uris: "http://localhost:4567/callback" },
      { token_endpoint_auth_method: "client_secret_basic" }
    ].each do |changes|
      stub_request(:get, CLIENT_ID).to_return(status: 200, body: description(**changes).to_json,
                                              headers: { "Content-Type" => "application/json" })

      assert error_page.present?, changes.inspect
    end
  end

  test "a description with no token_endpoint_auth_method is a public client" do
    publish(token_endpoint_auth_method: nil)
    sign_in_as @user

    assert_equal "Claude Code", approval_page.dig("client", "name")
  end

  test "a loopback redirect URI in the description matches any port and the three loopback hosts" do
    publish
    sign_in_as @user

    assert_equal "127.0.0.1", approval_page(redirect_uri: "http://127.0.0.1:9999/callback").dig("client", "redirect_host")
    assert error_page(redirect_uri: "http://127.0.0.1:9999/other").present?
  end

  test "a host that resolves to a loopback, private, link-local or multicast address is refused, and no request goes out" do
    sign_in_as @user
    publish
    [
      [ "127.0.0.1" ], [ "10.0.0.5" ], [ "172.16.0.1" ], [ "192.168.1.1" ], [ "169.254.169.254" ], [ "224.0.0.1" ],
      [ "0.0.0.0" ], [ "::1" ], [ "fc00::1" ], [ "fe80::1" ], [ "ff02::1" ], [ "::ffff:10.0.0.5" ],
      [ PUBLIC_ADDRESS, "10.0.0.5" ]
    ].each do |addresses|
      @addresses["client.example.com"] = addresses

      assert_match(/private or local address/, error_page, addresses.inspect)
    end
    assert_not_requested :get, CLIENT_ID
  end

  test "a host with no address shows the error page" do
    sign_in_as @user
    @addresses.delete("client.example.com")

    assert_match(/could not find the address/, error_page)
    assert_not_requested :get, CLIENT_ID
  end

  test "Console connects to the checked address, with no proxy, so DNS is not asked again" do
    http = Oauth::PublishedDescription.connection(URI(CLIENT_ID), PUBLIC_ADDRESS)

    assert_equal PUBLIC_ADDRESS, http.ipaddr
    assert_equal "client.example.com", http.address
    assert http.use_ssl?
    assert_not http.proxy?
  end

  test "a redirect, an error status, a document over 64 KB, a reply after 5 seconds and a reply that is not JSON are refused" do
    sign_in_as @user
    big = description.merge(padding: "a" * 64.kilobytes).to_json
    {
      { status: 301, headers: { "Location" => "https://elsewhere.example.com/client.json" } } => /redirect/,
      { status: 302, headers: { "Location" => CLIENT_ID } } => /redirect/,
      { status: 404 } => /HTTP 404/,
      { status: 200, body: big } => /64 KB/,
      { status: 200, body: big, headers: { "Content-Length" => "10" } } => /64 KB/,
      { status: 200, body: description.to_json, headers: { "Content-Length" => "abc" } } => /could not fetch/,
      { status: 200, body: "<html>Claude Code</html>" } => /not a JSON object/,
      { status: 200, body: "[1, 2]" } => /not a JSON object/,
    }.each do |reply, message|
      stub_request(:get, CLIENT_ID).to_return(reply)

      assert_match message, error_page, reply.except(:body).inspect
    end

    stub_request(:get, CLIENT_ID).to_timeout
    assert_match(/within 5 seconds/, error_page)

    stub_request(:get, CLIENT_ID).to_raise(Errno::ECONNREFUSED)
    assert_match(/could not fetch/, error_page)
  end

  test "the time limit covers the full reply: a host that sends header lines slowly is refused at the limit" do
    sign_in_as @user
    Oauth::PublishedDescription.time_limit = 0.5
    drip = lambda do |socket|
      socket.write("HTTP/1.1 200 OK\r\n")
      20.times do
        socket.write("X-Drip: 1\r\n")
        sleep 0.1
      end
    end

    started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    message = serve_locally(drip) { error_page }

    assert_match(/within 0.5 seconds/, message)
    assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 1.5
  end

  test "a host that sends a status line or headers without end is stopped at the byte limit, before the time limit" do
    sign_in_as @user
    Oauth::PublishedDescription.time_limit = 2
    endless_header = lambda do |socket|
      socket.write("HTTP/1.1 200 OK\r\nX-Big: ")
      200.times { socket.write("a" * 64.kilobytes) }
    end
    endless_headers = lambda do |socket|
      socket.write("HTTP/1.1 200 OK\r\n")
      20_000.times { |index| socket.write("X-Header-#{index}: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\n") }
    end

    [ endless_header, endless_headers ].each do |reply|
      started = Process.clock_gettime(Process::CLOCK_MONOTONIC)
      message = serve_locally(reply) { error_page }

      assert_match(/larger than 64 KB/, message)
      assert_operator Process.clock_gettime(Process::CLOCK_MONOTONIC) - started, :<, 1
    end
  end

  test "a document of exactly 64 KB, with usual headers, is read from the socket" do
    sign_in_as @user
    body = description.to_json
    body = body.sub(/\}\z/, %(,"padding":"#{'a' * (64.kilobytes - body.bytesize - 13)}"}))
    headers = "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nCache-Control: max-age=3600\r\n" \
              "X-Padding: #{'a' * 8.kilobytes}\r\nContent-Length: #{body.bytesize}\r\n\r\n"
    reply = ->(socket) { socket.write(headers + body) }

    assert_equal "Claude Code", serve_locally(reply) { approval_page }.dig("client", "name")
  end

  test "a reply with a gzip body that does not inflate is refused" do
    sign_in_as @user
    reply = ->(socket) { socket.write("HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Length: 9\r\n\r\nnot gzip!") }

    assert_match(/could not fetch/, serve_locally(reply) { error_page })
  end

  test "a document of exactly 64 KB is read" do
    sign_in_as @user
    body = description.to_json
    body = body.sub(/\}\z/, %(,"padding":"#{'a' * (64.kilobytes - body.bytesize - 13)}"}))
    assert_equal 64.kilobytes, body.bytesize
    stub_request(:get, CLIENT_ID).to_return(status: 200, body: body)

    assert_equal "Claude Code", approval_page.dig("client", "name")
  end

  # The kept copy

  test "Console does not fetch again before the cache time, and fetches again on the page after it" do
    publish(headers: { "Cache-Control" => "public, max-age=3600" })
    sign_in_as @user
    approval_page

    travel 59.minutes
    assert_equal "Claude Code", approval_page.dig("client", "name")
    assert_requested :get, CLIENT_ID, times: 1

    publish(client_name: "Claude Code 2")
    travel 2.minutes
    assert_equal "Claude Code 2", approval_page.dig("client", "name")
    assert_requested :get, CLIENT_ID, times: 2
  end

  test "the cache time is at least 5 minutes and at most 24 hours" do
    sign_in_as @user
    {
      { "Cache-Control" => "max-age=60" } => 5.minutes,
      { "Cache-Control" => "no-store, max-age=3600" } => 5.minutes,
      {} => 5.minutes,
      { "Cache-Control" => "max-age=604800" } => 24.hours,
    }.each_with_index do |(headers, lasts), index|
      client_id = "https://client.example.com/clients/#{index}.json"
      publish(client_id, headers: headers)
      approval_page(client_id: client_id)

      [ [ lasts - 1.minute, 1 ], [ lasts + 1.minute, 2 ] ].each do |later, fetches|
        travel(later) do
          sign_in_as @user # A Session does not last 24 hours.
          approval_page(client_id: client_id)
        end
        assert_requested :get, client_id, times: fetches
      end
    end
  end

  test "a fetch that fails when a fresh copy is needed shows the error page" do
    publish
    sign_in_as @user
    approval_page

    stub_request(:get, CLIENT_ID).to_timeout
    travel 2.hours

    assert_match(/within 5 seconds/, error_page)
  end

  test "the code exchange uses the kept copy: with the description's host down it still works" do
    publish
    code = approved_code
    stub_request(:get, CLIENT_ID).to_raise(Errno::ECONNREFUSED)
    travel 2.hours # The copy is old; the token endpoint still does not fetch.
    travel_back
    WebMock.reset_executed_requests!

    exchange(code)

    assert_response :ok
    assert_match(/\Asto_/, response.parsed_body["access_token"])
    assert_not_requested :get, CLIENT_ID
  end

  test "the token endpoint does not know a published client_id that no signed-in user has fetched" do
    publish

    exchange("some-code")

    assert_response :unauthorized
    assert_equal "invalid_client", response.parsed_body["error"]
    assert_not_requested :get, CLIENT_ID
  end

  # Fetch limits

  test "fetches are limited to 10 in 3 minutes for each user" do
    Oauth::PublishedDescription.limit_store = ActiveSupport::Cache::MemoryStore.new
    sign_in_as @user
    10.times do |index|
      client_id = "https://client.example.com/clients/#{index}.json"
      publish(client_id)
      approval_page(client_id: client_id)
    end
    publish("https://client.example.com/clients/10.json")

    assert_match(/too many clients/, error_page(client_id: "https://client.example.com/clients/10.json"))
    assert_not_requested :get, "https://client.example.com/clients/10.json"

    # A kept fresh copy needs no fetch, so it is not limited.
    approval_page(client_id: "https://client.example.com/clients/0.json")

    # Another user is not limited.
    other = create(:user, email: "bob@example.com")
    create(:organization_user, organization: @acme, user: other)
    open_console_at
    sign_in_as other
    approval_page(client_id: "https://client.example.com/clients/10.json")

    travel 3.minutes + 1.second
    open_console_at
    sign_in_as @user
    publish("https://client.example.com/clients/11.json")
    approval_page(client_id: "https://client.example.com/clients/11.json")
  end

  test "only one fetch at a time runs for one client_id: a request in that time gets the kept copy, or Try again in a moment" do
    other = create(:user, email: "bob@example.com")
    busy = nil
    stub_request(:get, CLIENT_ID).to_return do
      busy = assert_raises(Oauth::PublishedDescription::Problem) { Oauth::PublishedDescription.client(CLIENT_ID, user: other) }
      { status: 200, body: description.to_json }
    end
    sign_in_as @user

    approval_page
    assert_equal "Console is checking this client now. Try again in a moment.", busy.message

    kept = nil
    stub_request(:get, CLIENT_ID).to_return do
      kept = Oauth::PublishedDescription.client(CLIENT_ID, user: other)
      { status: 200, body: description(client_name: "Claude Code 2").to_json }
    end
    travel 2.hours

    assert_equal "Claude Code 2", approval_page.dig("client", "name")
    assert_equal "Claude Code", kept.name
    assert_requested :get, CLIENT_ID, times: 2
  end

  test "a stopped fetch does not block the next one" do
    sign_in_as @user
    stub_request(:get, CLIENT_ID).to_raise(Errno::ECONNREFUSED)
    error_page
    publish

    assert_equal "Claude Code", approval_page.dig("client", "name")
  end

  # The full browser grant

  test "the full browser grant with a published client ends in a working POST /mcp whoami" do
    freeze_time
    secret = with_authenticator_app
    publish

    get oauth_authorize_url(authorize_params)
    assert_redirected_to login_url
    assert_not_requested :get, CLIENT_ID

    post login_url, params: { email: @user.email, password: "password" }
    post second_step_url, params: { code: code_for(secret) }
    get response.location, headers: inertia_headers
    props = response.parsed_body["props"]
    assert_equal "Checked: published at client.example.com", props.dig("client", "name_note")

    approve(props["request"])
    assert_response :conflict
    code = redirect_query["code"]
    assert_equal ISSUER, redirect_query["iss"]

    stub_request(:get, CLIENT_ID).to_raise(Errno::ECONNREFUSED)
    exchange(code)
    assert_response :ok
    mcp "tools/call", token: response.parsed_body["access_token"], name: "whoami", arguments: {}

    assert_response :ok
    answer = JSON.parse(response.parsed_body.dig("result", "content", 0, "text").split("\nConsole: ").first)
    assert_equal [ { "slug" => "acme", "name" => "Acme", "role" => "admin" } ], answer["organizations"]
    assert_requested :get, CLIENT_ID, times: 1
  end

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

  def approve(key)
    post oauth_authorize_url, params: { request: key, organization_id: @acme.id, decision: "approve" },
                              headers: inertia_headers
  end

  # The query of the URL that the last Approve sent the browser to.
  def redirect_query
    URI.decode_www_form(URI(response.headers["X-Inertia-Location"]).query).to_h
  end

  # A code from a signed-in Approve.
  def approved_code
    sign_in_as @user
    approve(approval_page["request"])
    assert_response :conflict
    redirect_query["code"]
  end

  def exchange(code)
    post oauth_token_url, params: { grant_type: "authorization_code", code: code, code_verifier: VERIFIER,
                                    client_id: CLIENT_ID, redirect_uri: REDIRECT_URI, resource: RESOURCE }
  end

  def mcp(method, token:, **params)
    headers = { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json",
                "Authorization" => "Bearer #{token}" }
    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: method, params: params }.to_json, headers: headers
  end

  # Runs the block with a plain-HTTP host on this computer in place of the
  # client's HTTPS host. The host reads the request, then reply writes the
  # answer to the socket. The block's value is returned.
  def serve_locally(reply)
    server = TCPServer.new("127.0.0.1", 0)
    host = Thread.new do
      socket = server.accept
      socket.gets("\r\n\r\n")
      reply.call(socket)
    rescue IOError, SystemCallError
      nil
    ensure
      socket&.close
    end
    connection = lambda do |_uri, _address|
      Net::HTTP.new("127.0.0.1", server.addr[1], nil).tap do |http|
        http.open_timeout = http.read_timeout = Oauth::PublishedDescription::TIMEOUT
        http.max_retries = 0
      end
    end
    WebMock.disable_net_connect!(allow_localhost: true)
    Oauth::PublishedDescription.stub(:connection, connection) { yield }
  ensure
    WebMock.disable_net_connect!
    host&.kill
    server&.close
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
