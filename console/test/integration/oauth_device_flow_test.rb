# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The device flow (RFC 8628), for clients with a published description
# only. A program with no browser asks for a device code and shows the
# user code and the device page's URL; the user signs in on another device,
# types the code, sees where the program's description is published and
# when and from which IP address it asked, and approves for one
# Organization. The program polls the token endpoint for its tokens and
# then calls POST /mcp. WebMock serves the description; the host's
# addresses come from a stubbed resolver.
class OauthDeviceFlowTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include TwoFactorTestHelper

  ISSUER = PasskeyTestHelper::CONSOLE_URL
  RESOURCE = "#{ISSUER}/mcp".freeze
  CLIENT_ID = "https://cli.example.com/oauth/client.json"
  DEVICE_GRANT = "urn:ietf:params:oauth:grant-type:device_code"
  PUBLIC_ADDRESS = "198.51.100.92"
  PROGRAM_ADDRESS = "203.0.113.7"
  USER_CODE = /\A[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}\z/

  # Time stands still unless a test moves it, so the 10 minutes and the poll
  # interval are exact.
  setup do
    freeze_time
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:dashboard, organization: @acme, name: "Shop errors")
    open_console_at
    @resolver = Oauth::PublishedDescription.resolver
    Oauth::PublishedDescription.resolver = ->(host) {
      { "cli.example.com" => [ PUBLIC_ADDRESS ] }.fetch(host) { raise SocketError, "unknown host" }
    }
  end

  teardown do
    Oauth::PublishedDescription.resolver = @resolver
  end

  # Device authorization

  test "a client with a published description gets a device code, a user code and the device page's URL" do
    ask_for_device_code

    assert_response :ok
    body = response.parsed_body
    assert_equal %w[device_code expires_in interval user_code verification_uri], body.keys.sort
    assert_match(/\A[A-Za-z0-9_-]{43}\z/, body["device_code"])
    assert_match USER_CODE, body["user_code"]
    assert_equal "#{ISSUER}/device", body["verification_uri"]
    assert_equal 600, body["expires_in"]
    assert_equal 5, body["interval"]
    assert_equal "no-store", response.headers["Cache-Control"]
    assert_equal "*", response.headers["Access-Control-Allow-Origin"]
  end

  test "a device code and its user code are kept only as digests" do
    answer = device_code_answer

    code = Oauth::Code.sole
    user_code = answer["user_code"].delete("-")
    assert_equal Oauth::Secret.digest(answer["device_code"]), code.code_digest
    assert_equal Oauth::Secret.digest(user_code), code.user_code_digest
    values = code.attributes.values.map(&:to_s)
    [ answer["device_code"], answer["user_code"], user_code ].each do |secret|
      assert values.none? { |value| value.include?(secret) }, "#{secret} is kept in plain text"
    end
  end

  test "a new device code deletes the device codes that expired more than an hour ago" do
    old = device_code_answer
    travel 70.minutes
    recent = device_code_answer
    travel 10.minutes + 1.second

    ask_for_device_code

    assert_response :ok
    digests = Oauth::Code.where(kind: "device").pluck(:code_digest)
    assert_not_includes digests, Oauth::Secret.digest(old["device_code"])
    assert_includes digests, Oauth::Secret.digest(recent["device_code"])
    assert_equal 2, digests.size
  end

  test "device authorization fetches no description" do
    publish

    ask_for_device_code

    assert_response :ok
    assert_not_requested :any, /.*/
  end

  test "each device code and user code is new" do
    answers = 3.times.map { ask_for_device_code && response.parsed_body }

    assert_equal 3, answers.map { |answer| answer["device_code"] }.uniq.size
    assert_equal 3, answers.map { |answer| answer["user_code"] }.uniq.size
  end

  test "a user code is made again until no live device code has it" do
    first = ask_for_device_code && response.parsed_body["user_code"].delete("-")
    letters = (first + "BBBBBBBB").chars
    Oauth::DeviceCode.stub(:random_letter, -> { letters.shift }) do
      ask_for_device_code
    end

    assert_response :ok
    assert_equal "BBBB-BBBB", response.parsed_body["user_code"]
  end

  test "a user code of an expired device code can be given again" do
    first = ask_for_device_code && response.parsed_body["user_code"].delete("-")
    travel 10.minutes
    letters = first.chars
    Oauth::DeviceCode.stub(:random_letter, -> { letters.shift }) do
      ask_for_device_code
    end

    assert_response :ok
    assert_equal first, response.parsed_body["user_code"].delete("-")
  end

  test "a registered (DCR) client cannot use the device flow" do
    post oauth_register_url, params: { client_name: "CLI", redirect_uris: [ "http://localhost:4567/callback" ] }.to_json,
                             headers: { "Content-Type" => "application/json" }
    client_id = response.parsed_body["client_id"]

    ask_for_device_code(client_id: client_id)

    assert_oauth_error "unauthorized_client"
  end

  test "an unknown client_id, or one of a bad form, gets invalid_client" do
    [ "unknown", "http://cli.example.com/oauth/client.json", "https://cli.example.com/", "https://10.0.0.5/client.json",
      nil ].each do |client_id|
      ask_for_device_code(client_id: client_id)

      assert_oauth_error "invalid_client", status: :unauthorized
    end
  end

  test "a stored fresh description that does not list the device grant gets unauthorized_client" do
    keep_description(grant_types: %w[authorization_code refresh_token])

    ask_for_device_code

    assert_oauth_error "unauthorized_client"
    assert_not_requested :any, /.*/
  end

  test "a stored description that is no longer fresh is checked again on the device page, not here" do
    keep_description(grant_types: %w[authorization_code refresh_token])
    travel 2.hours

    ask_for_device_code

    assert_response :ok
  end

  test "a stored fresh description that lists the device grant gets a device code" do
    keep_description

    ask_for_device_code

    assert_response :ok
  end

  test "an unknown scope gets invalid_scope, and a resource of no scope invalid_target" do
    ask_for_device_code(scope: "mcp logs")
    assert_oauth_error "invalid_scope"

    ask_for_device_code(resource: "#{ISSUER}/logs")
    assert_oauth_error "invalid_target"
  end

  test "with OAuth off device authorization answers invalid_request" do
    InstanceSetting.current.update!(console_url: nil)

    ask_for_device_code

    assert_oauth_error "invalid_request"
    assert_match "HTTPS Console URL", response.parsed_body["error_description"]
  end

  test "device authorization is limited to 60 a minute for each IP address and for each client_id" do
    with_limits(Oauth::DeviceAuthorizationsController) do
      60.times { ask_for_device_code }
      assert_response :ok

      ask_for_device_code
      assert_oauth_error "invalid_request", status: :too_many_requests

      # Another client_id from another address: a new count.
      ask_for_device_code(client_id: "https://other.example.com/client.json", ip: "203.0.113.8")
      assert_response :ok

      # The same client_id from another address: the client's count is full.
      ask_for_device_code(ip: "203.0.113.9")
      assert_oauth_error "invalid_request", status: :too_many_requests
    end
  end

  # The device page

  test "the device page needs a Session, and sign-in comes back to it without the code in the URL" do
    get device_url(user_code: "BCDF-GHJK")

    assert_redirected_to login_url
    assert_equal "/device", session[:return_to_after_authenticating]

    sign_in_as @user
    assert_redirected_to "#{ISSUER}/device"
  end

  test "the device page's field is empty even when the URL carries a code, and it says to type only your own code" do
    sign_in_as @user

    get device_url(user_code: "BCDF-GHJK"), headers: inertia_headers

    assert_response :ok
    assert_equal "Oauth/Device", response.parsed_body["component"]
    props = response.parsed_body["props"]
    assert_equal "Type a code only from a program that you started yourself.", props["warning"]
    assert_nil props["error"]
    assert_not_includes props.to_json, "BCDF"
  end

  test "with OAuth off the device page shows the error page" do
    InstanceSetting.current.update!(console_url: nil)
    sign_in_as @user

    get device_url, headers: inertia_headers

    assert_equal "Oauth/Error", response.parsed_body["component"]
    assert_match "HTTPS Console URL", response.parsed_body.dig("props", "message")
  end

  test "a wrong code shows an error on the device page and fetches nothing" do
    publish
    right = device_code_answer["user_code"]
    sign_in_as @user

    [ wrong_code(right), "BCDF", "BCDF-GHJK-L", "AEIO-UBCD", "", nil ].each do |typed|
      enter typed

      assert_response :unprocessable_entity, typed.inspect
      assert_equal "Oauth/Device", response.parsed_body["component"]
      assert_equal "That code is wrong or expired. Check the code in the program, and type it again.",
        response.parsed_body.dig("props", "error")
    end
    assert_not_requested :get, CLIENT_ID
  end

  test "the code compares without letter case, dash and spaces" do
    publish
    user_code = device_code_answer["user_code"]
    sign_in_as @user

    [ user_code.downcase.delete("-"), " #{user_code.downcase} ", user_code.sub("-", " ") ].each do |typed|
      enter typed

      assert_response :redirect, typed
      assert_match %r{\A#{Regexp.escape(ISSUER)}/device\?request=}, response.location
    end
  end

  test "device code entries are limited to 10 in 3 minutes for each user and for each IP address" do
    other = create(:user, name: "Bob", email: "bob@example.com")
    create(:organization_user, organization: @acme, user: other, role: :member)
    with_limits(Oauth::DevicesController) do
      sign_in_as @user
      10.times { enter "BCDF-GHJK" }
      assert_response :unprocessable_entity

      enter "BCDF-GHJK"
      assert_response :too_many_requests
      assert_equal "Oauth/Error", response.parsed_body["component"]

      # The same user from another address.
      enter "BCDF-GHJK", ip: "203.0.113.20"
      assert_response :too_many_requests

      # Another user from the first address.
      sign_in_as other
      enter "BCDF-GHJK"
      assert_response :too_many_requests

      # Another user from another address.
      enter "BCDF-GHJK", ip: "203.0.113.21"
      assert_response :unprocessable_entity
    end
  end

  test "a right code shows the client, where it is published, when and from where the program asked, and the choices" do
    publish
    asked_at = Time.current
    answer = device_code_answer
    travel 2.minutes
    sign_in_as @user

    props = approval_page(answer["user_code"])

    assert_equal "Trace CLI", props.dig("client", "name")
    assert_equal "Published at cli.example.com", props.dig("client", "name_note")
    assert_equal asked_at.iso8601, props["asked_at"]
    assert_equal PROGRAM_ADDRESS, props["asked_from"]
    assert_equal [ { "id" => @acme.id, "name" => "Acme", "slug" => "acme",
                     "scopes" => [ "Use the MCP server as you, in Acme: read its Logs and Issues, and change its " \
                                   "Dashboards, Monitors, Issues and Projects as your role allows." ],
                     "two_factor_note" => nil } ], props["organizations"]
    assert_nil props["no_organization"]
    assert_equal "You can revoke this in Settings → User.", props["revoke_hint"]
    assert_not_includes response.body, answer["user_code"].delete("-")
    assert_requested :get, CLIENT_ID, times: 1
  end

  test "a description with only the device grant and no redirect_uris is accepted for the device flow" do
    publish(grant_types: [ DEVICE_GRANT ])
    user_code = device_code_answer["user_code"]
    sign_in_as @user

    assert_equal "Trace CLI", approval_page(user_code).dig("client", "name")
  end

  test "a bad description shows the error page and denies the device code" do
    [
      [ { client_id: "https://cli.example.com/other.json" }, "names another client_id" ],
      [ { grant_types: %w[authorization_code refresh_token], redirect_uris: [ "http://localhost:4567/callback" ] },
        "does not list the device grant" ],
      [ { token_endpoint_auth_method: "client_secret_basic" }, "confidential client" ],
      [ { grant_types: [ DEVICE_GRANT, "authorization_code" ] }, "gives no https:// or loopback redirect_uris" ]
    ].each do |changes, message|
      Oauth::Client.delete_all
      publish(**changes)
      answer = device_code_answer
      sign_in_as @user

      enter answer["user_code"]

      assert_response :bad_request, message
      assert_equal "Oauth/Error", response.parsed_body["component"]
      assert_match message, response.parsed_body.dig("props", "message")
      poll answer["device_code"]
      assert_oauth_error "access_denied"
    end
  end

  test "a description that cannot be fetched shows the error page and denies the device code" do
    stub_request(:get, CLIENT_ID).to_return(status: 404)
    answer = device_code_answer
    sign_in_as @user

    enter answer["user_code"]

    assert_equal "Oauth/Error", response.parsed_body["component"]
    poll answer["device_code"]
    assert_oauth_error "access_denied"
  end

  test "a host that does not answer shows the error page, and the device code still waits and works later" do
    answer = device_code_answer
    sign_in_as @user
    [
      [ -> { stub_request(:get, CLIENT_ID).to_timeout }, "within 5 seconds" ],
      [ -> { stub_request(:get, CLIENT_ID).to_raise(Errno::ECONNREFUSED) }, "could not fetch" ],
      [ -> { stub_request(:get, CLIENT_ID).to_return(status: 503) }, "HTTP 503" ]
    ].each do |stub, message|
      stub.call

      enter answer["user_code"]

      assert_response :bad_request, message
      assert_equal "Oauth/Error", response.parsed_body["component"]
      assert_match message, response.parsed_body.dig("props", "message")
      assert_match "Try again later", response.parsed_body.dig("props", "message")
      travel 5.seconds
      poll answer["device_code"]
      assert_oauth_error "authorization_pending"
    end

    publish
    assert_equal "Trace CLI", approval_page(answer["user_code"]).dig("client", "name")
  end

  test "while another fetch of the description runs, the page says to try again and the device code still waits" do
    Oauth::Client.create!(kind: "cimd", client_id: CLIENT_ID, name: "cli.example.com", redirect_uris: [], grant_types: [],
                          fetch_started_at: Time.current)
    answer = device_code_answer
    sign_in_as @user

    enter answer["user_code"]

    assert_equal "Oauth/Error", response.parsed_body["component"]
    assert_equal Oauth::PublishedDescription::BUSY, response.parsed_body.dig("props", "message")
    poll answer["device_code"]
    assert_oauth_error "authorization_pending"
  end

  test "a user in no Organization sees why, and Approve makes no Grant" do
    OrganizationUser.where(user: @user).delete_all
    publish
    user_code = device_code_answer["user_code"]
    sign_in_as @user

    props = approval_page(user_code)

    assert_equal [], props["organizations"]
    assert_match "You are in no Organization", props["no_organization"]
    decide props["request"], "approve"
    assert_response :not_found
    assert_equal 0, Oauth::Grant.count
  end

  test "the approval answers only in the browser that typed the code" do
    publish
    key = approval_page_for_new_code["request"]
    other = create(:user, name: "Bob", email: "bob@example.com")
    create(:organization_user, organization: @acme, user: other, role: :admin)

    open_session do |browser|
      browser.post login_url, params: { email: other.email, password: "password" }
      browser.get device_url(request: key), headers: inertia_headers
      assert_equal "Oauth/Error", browser.response.parsed_body["component"]
      browser.post device_url, params: { request: key, organization_id: @acme.id, decision: "approve" },
                               headers: inertia_headers
      assert_equal "Oauth/Error", browser.response.parsed_body["component"]
    end
    assert_equal 0, Oauth::Grant.count
  end

  test "a device page request does not work on the approval page" do
    publish
    key = approval_page_for_new_code["request"]

    get oauth_authorize_url(request: key), headers: inertia_headers
    assert_equal "Oauth/Error", response.parsed_body["component"]
    post oauth_authorize_url, params: { request: key, organization_id: @acme.id, decision: "approve" },
                              headers: inertia_headers
    assert_equal "Oauth/Error", response.parsed_body["component"]
    assert_equal 0, Oauth::Grant.count
  end

  test "Approve after the device code expired shows the error page and makes no Grant" do
    publish
    answer = device_code_answer
    sign_in_as @user
    key = approval_page(answer["user_code"])["request"]
    travel 10.minutes

    decide key, "approve"

    assert_equal "Oauth/Error", response.parsed_body["component"]
    assert_equal 0, Oauth::Grant.count
  end

  test "of two users who type one code, only the first decision counts" do
    publish
    answer = device_code_answer
    other = create(:user, name: "Bob", email: "bob@example.com")
    create(:organization_user, organization: @acme, user: other, role: :admin)
    sign_in_as @user
    key = approval_page(answer["user_code"])["request"]

    open_session do |browser|
      browser.post login_url, params: { email: other.email, password: "password" }
      browser.post device_url, params: { user_code: answer["user_code"] }, headers: inertia_headers
      other_key = URI.decode_www_form(URI(browser.response.location).query).to_h["request"]
      browser.post device_url, params: { request: other_key, organization_id: @acme.id, decision: "deny" },
                               headers: inertia_headers
      assert_equal "Oauth/Device", browser.response.parsed_body["component"]
    end

    decide key, "approve"

    assert_equal "Oauth/Error", response.parsed_body["component"]
    assert_equal 0, Oauth::Grant.count
    poll answer["device_code"]
    assert_oauth_error "access_denied"
  end

  test "Approve for an Organization with Require two-factor sends a user with no Two-factor to set it up, and back" do
    @acme.update!(require_two_factor: true)
    publish
    answer = device_code_answer
    sign_in_as @user
    key = approval_page(answer["user_code"])["request"]

    decide key, "approve"

    assert_redirected_to new_authenticator_app_url(org: "acme")
    assert_equal 0, Oauth::Grant.count
    secret = set_up_page_for("acme")["secret"]
    post authenticator_app_url, params: { code: code_for(secret), org: "acme", proof: { password: "password" } }
    assert_equal device_path(request: key), recovery_codes_page["continue_to"]

    get device_url(request: key), headers: inertia_headers
    assert_equal "Oauth/DeviceApproval", response.parsed_body["component"]
    decide key, "approve"
    assert_equal "Oauth/Device", response.parsed_body["component"]
    poll answer["device_code"]
    assert_response :ok
  end

  test "Approve ends the request, and the kept return to it from a Two-factor set-up" do
    @acme.update!(require_two_factor: true)
    beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: beta, user: @user, role: :admin)
    publish
    sign_in_as @user
    key = approval_page(device_code_answer["user_code"])["request"]
    decide key, "approve"
    assert_redirected_to new_authenticator_app_url(org: "acme")

    decide key, "approve", organization: beta

    assert_equal "Oauth/Device", response.parsed_body["component"]
    assert_equal 0, Oauth::PendingRequest.where(key: key).count
    assert_not_includes Array(session[Oauth::PendingRequest::SESSION_KEY]), key
    secret = set_up_page_for("acme")["secret"]
    post authenticator_app_url, params: { code: code_for(secret), org: "acme", proof: { password: "password" } }
    assert_not_equal device_path(request: key), recovery_codes_page["continue_to"]
  end

  # Polling and the tokens

  test "after Approve the program gets its tokens once, and the access token works at POST /mcp" do
    publish
    answer = device_code_answer
    sign_in_as @user
    key = approval_page(answer["user_code"])["request"]

    decide key, "approve", user_agent: "Firefox on a phone"

    assert_response :ok
    assert_equal "Oauth/Device", response.parsed_body["component"]
    assert_equal "Approved. Go back to the program: it signs in within a few seconds.",
      response.parsed_body.dig("props", "done")
    poll answer["device_code"]
    assert_response :ok
    tokens = response.parsed_body
    assert_equal %w[access_token expires_in refresh_token scope token_type], tokens.keys.sort
    assert_match(/\Asto_/, tokens["access_token"])
    assert_equal [ "Bearer", 3600, "mcp" ], tokens.values_at("token_type", "expires_in", "scope")

    travel 5.seconds
    poll answer["device_code"]
    assert_oauth_error "invalid_grant"

    mcp "tools/call", token: tokens["access_token"], name: "list_dashboards", arguments: {}
    assert_response :ok
    assert_not response.parsed_body.dig("result", "isError")
    assert_match "Shop errors", response.parsed_body.dig("result", "content", 0, "text")
    grant = Oauth::Grant.sole
    assert_equal [ "device", @user, @acme, "Firefox on a phone" ],
      [ grant.approved_via, grant.user, grant.organization, grant.user_agent ]
  end

  test "the refresh token from the device flow gets a new pair, with no fetch of the description" do
    publish
    answer = device_code_answer
    sign_in_as @user
    decide approval_page(answer["user_code"])["request"], "approve"
    poll answer["device_code"]
    refresh_token = response.parsed_body["refresh_token"]

    post oauth_token_url, params: { grant_type: "refresh_token", refresh_token: refresh_token, client_id: CLIENT_ID }

    assert_response :ok
    assert_match(/\Asto_/, response.parsed_body["access_token"])
    assert_requested :get, CLIENT_ID, times: 1
  end

  test "a poll after the Grant was revoked or the user left the Organization gets invalid_grant and no tokens" do
    publish
    sign_in_as @user
    revoked = device_code_answer
    decide approval_page(revoked["user_code"])["request"], "approve"
    left = device_code_answer
    decide approval_page(left["user_code"])["request"], "approve"
    revoked_grant, = Oauth::Grant.order(:id).to_a

    revoked_grant.revoke!
    poll revoked["device_code"]
    assert_oauth_error "invalid_grant"

    OrganizationUser.where(user: @user, organization: @acme).delete_all
    poll left["device_code"]
    assert_oauth_error "invalid_grant"
    assert_equal 0, Oauth::Token.count
  end

  test "polling answers authorization_pending before a decision, and access_denied after Deny" do
    publish
    answer = device_code_answer
    poll answer["device_code"]
    assert_oauth_error "authorization_pending"

    sign_in_as @user
    key = approval_page(answer["user_code"])["request"]
    travel 5.seconds
    poll answer["device_code"]
    assert_oauth_error "authorization_pending"

    decide key, "deny"
    assert_equal "Denied. The program gets no access.", response.parsed_body.dig("props", "done")
    travel 5.seconds
    poll answer["device_code"]
    assert_oauth_error "access_denied"
    assert_equal 0, Oauth::Grant.count
  end

  test "polling faster than the interval answers slow_down and adds 5 seconds to the interval" do
    device_code = device_code_answer["device_code"]
    poll device_code
    assert_oauth_error "authorization_pending"

    travel 5.seconds
    poll device_code
    assert_oauth_error "authorization_pending"

    travel 4.seconds
    poll device_code
    assert_oauth_error "slow_down"
    assert_match "10 seconds", response.parsed_body["error_description"]

    travel 9.seconds
    poll device_code
    assert_oauth_error "slow_down"
    assert_match "15 seconds", response.parsed_body["error_description"]

    travel 15.seconds
    poll device_code
    assert_oauth_error "authorization_pending"
  end

  test "a device code expires after 10 minutes" do
    publish
    answer = device_code_answer
    travel 10.minutes - 1.second
    poll answer["device_code"]
    assert_oauth_error "authorization_pending"

    travel 1.second
    poll answer["device_code"]
    assert_oauth_error "expired_token"

    sign_in_as @user
    enter answer["user_code"]
    assert_response :unprocessable_entity
  end

  test "a poll with an unknown device code or another client_id gets invalid_grant, and a device code is no authorization code" do
    keep_description
    device_code = device_code_answer["device_code"]

    poll "x" * 43
    assert_oauth_error "invalid_grant"
    poll device_code, client_id: "https://other.example.com/client.json"
    assert_oauth_error "invalid_grant"
    post oauth_token_url, params: { grant_type: "authorization_code", code: device_code, client_id: CLIENT_ID,
                                    code_verifier: "a" * 43, redirect_uri: "http://localhost:4567/callback" }
    assert_oauth_error "invalid_grant"
  end

  test "a poll with no client_id or with a client secret gets invalid_client" do
    device_code = device_code_answer["device_code"]

    poll device_code, client_id: nil
    assert_oauth_error "invalid_client", status: :unauthorized
    poll device_code, client_secret: "secret"
    assert_oauth_error "invalid_client", status: :unauthorized
    poll device_code, headers: { "Authorization" => "Basic #{Base64.strict_encode64("#{CLIENT_ID}:")}" }
    assert_oauth_error "invalid_client", status: :unauthorized
  end

  test "a device poll, a device code entry and Approve write no device code, user code or token to the request log" do
    publish
    answer = nil
    tokens = nil
    log = log_of do
      answer = device_code_answer
      sign_in_as @user
      key = approval_page(answer["user_code"])["request"]
      decide key, "approve"
      poll answer["device_code"]
      tokens = response.parsed_body
    end

    assert_match "/oauth/token", log
    assert_match "/device", log
    [ answer["device_code"], answer["user_code"], answer["user_code"].delete("-"), tokens["access_token"],
      tokens["refresh_token"] ].each do |secret|
      assert_not_includes log, secret
    end
  end

  private

  def description(**changes)
    { client_id: CLIENT_ID, client_name: "Trace CLI", grant_types: [ DEVICE_GRANT, "refresh_token" ],
      token_endpoint_auth_method: "none" }.merge(changes).compact
  end

  # Serves the description at CLIENT_ID.
  def publish(headers: { "Cache-Control" => "max-age=3600" }, **changes)
    stub_request(:get, CLIENT_ID).to_return(status: 200, body: description(**changes).to_json,
                                            headers: { "Content-Type" => "application/json" }.merge(headers))
  end

  # A kept copy of the description, fresh for an hour, as an earlier
  # signed-in page leaves it; no request goes out in the test.
  def keep_description(**changes)
    Oauth::Client.create!(kind: "cimd", client_id: CLIENT_ID, name: "cli.example.com", redirect_uris: [], grant_types: [])
      .keep_description!(description(**changes).stringify_keys, fresh_for: 1.hour)
  end

  # POST /oauth/device_authorization from the program's address.
  def ask_for_device_code(client_id: CLIENT_ID, ip: PROGRAM_ADDRESS, **params)
    post oauth_device_authorization_url, params: { client_id: client_id, **params }.compact,
                                         headers: { "REMOTE_ADDR" => ip }
    true
  end

  # A device code answer for CLIENT_ID.
  def device_code_answer
    ask_for_device_code
    assert_response :ok
    response.parsed_body
  end

  # A user code of the right form that is not code.
  def wrong_code(code)
    code == "BCDF-GHJK" ? "BCDF-GHJL" : "BCDF-GHJK"
  end

  # Types a user code on the device page.
  def enter(user_code, ip: "127.0.0.1")
    post device_url, params: { user_code: user_code }, headers: inertia_headers.merge("REMOTE_ADDR" => ip)
  end

  # The approval props on the device page after a right user code.
  def approval_page(user_code)
    enter user_code
    assert_response :redirect
    get response.location, headers: inertia_headers
    assert_response :ok
    assert_equal "Oauth/DeviceApproval", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def approval_page_for_new_code
    user_code = device_code_answer["user_code"]
    sign_in_as @user
    approval_page(user_code)
  end

  def decide(key, decision, organization: @acme, user_agent: "Firefox")
    post device_url, params: { request: key, organization_id: organization.id, decision: decision },
                     headers: inertia_headers.merge("User-Agent" => user_agent)
  end

  # The program's poll of the token endpoint.
  def poll(device_code, client_id: CLIENT_ID, headers: {}, **params)
    post oauth_token_url, params: { grant_type: DEVICE_GRANT, device_code: device_code, client_id: client_id, **params }.compact,
                          headers: { "REMOTE_ADDR" => PROGRAM_ADDRESS }.merge(headers)
  end

  def mcp(method, token:, **params)
    headers = { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json",
                "Authorization" => "Bearer #{token}" }
    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: method, params: params }.to_json, headers: headers
  end

  def set_up_page_for(org)
    get new_authenticator_app_url(org: org), headers: inertia_headers
    assert_equal "Settings/TwoFactorSetUp", response.parsed_body["component"]
    response.parsed_body["props"]
  end

  # The request log's lines for the block's requests and redirects.
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

  def assert_oauth_error(error, status: :bad_request)
    assert_response status
    assert_equal error, response.parsed_body["error"]
    assert response.parsed_body["error_description"].present?
  end

  # Runs the block with the controller's limits counted in a memory store.
  def with_limits(controller, &block)
    store = ActiveSupport::Cache::MemoryStore.new
    controller.cache_store.stub(:increment, ->(*args, **options) { store.increment(*args, **options) }, &block)
  end
end
