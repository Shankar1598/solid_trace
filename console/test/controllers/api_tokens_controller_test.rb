# frozen_string_literal: true

require "test_helper"

class ApiTokensControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user
    @other = create(:user)
    @others_token, = ApiToken.issue!(user: @other, name: "Theirs")

    sign_in_as(@user)
  end

  test "Settings → API tokens & MCP lists only the user's own tokens, by name, creation and last use" do
    mine, = ApiToken.issue!(user: @user, name: "Laptop")
    mine.touch_last_used

    props = inertia_props(tokens_page_url)

    assert_equal [ "Laptop" ], props["api_tokens"].map { |t| t["name"] }
    listed = props["api_tokens"].first
    assert_equal({ "id" => mine.id, "name" => "Laptop", "created_at" => mine.created_at.iso8601,
                   "last_used_at" => mine.reload.last_used_at.iso8601,
                   "scopes" => [], "organization" => nil, "limits" => {}, "expires_at" => nil, "dispatcher" => false }, listed)
  end

  test "the page gives the MCP URL at Console's URL: the setting, else as the request sees it" do
    assert_equal "http://www.example.com/mcp", inertia_props(tokens_page_url)["mcp_url"]

    InstanceSetting.current.update!(console_url: "https://solid-trace.example.ts.net")
    assert_equal "https://solid-trace.example.ts.net/mcp", inertia_props(tokens_page_url)["mcp_url"]
  end

  test "the page notes that sign-in through the browser needs an HTTPS Console URL while OAuth is off, and not when on" do
    note = inertia_props(tokens_page_url)["browser_sign_in_note"]
    assert_match "Sign-in through the browser needs an HTTPS Console URL", note
    # Settings → Instance cannot save an https:// Console URL while Console
    # runs without HTTPS, so the note gives CONSOLE_URL and a restart.
    assert_match "sets CONSOLE_URL to an https:// address in deploy/.env and restarts Console", note
    settings = InstanceSetting.current.tap { |s| s.console_url = "https://console.example.com" }
    assert_not settings.valid?(:editable)
    assert_match "must start with http://", settings.errors[:console_url].first

    InstanceSetting.current.tap { |settings| settings.console_url = "http://console.example.com" }.save!(validate: false)
    assert_equal note, inertia_props(tokens_page_url)["browser_sign_in_note"]

    [ "https://console.example.com", "http://localhost:4001" ].each do |url|
      InstanceSetting.current.update!(console_url: url)
      props = inertia_props(tokens_page_url)
      assert props.key?("browser_sign_in_note")
      assert_nil props["browser_sign_in_note"], url
    end
  end

  # The page shows the sign-in snippet with mcp_url when there is no note. A
  # client signs in only at the Console URL's host, so mcp_url must be the
  # resource that the 401 and the metadata name, also when the page is
  # opened on another host and the Console URL comes from CONSOLE_URL.
  test "with OAuth on, the MCP URL is the resource at the Console URL's host, not the request's" do
    ENV["CONSOLE_URL"] = "https://console.example.com"
    props = inertia_props(tokens_page_url)
    assert_nil props["browser_sign_in_note"]
    assert_equal "https://console.example.com/mcp", props["mcp_url"]

    InstanceSetting.current.update!(console_url: "https://solid-trace.example.ts.net/")
    props = inertia_props(tokens_page_url)
    assert_nil props["browser_sign_in_note"]
    assert_equal "https://solid-trace.example.ts.net/mcp", props["mcp_url"]

    post "/mcp", params: { jsonrpc: "2.0", id: 1, method: "tools/list" }.to_json,
      headers: { "Content-Type" => "application/json", "Accept" => "application/json, text/event-stream" }
    assert_response :unauthorized
    assert_includes response.headers["WWW-Authenticate"],
      %(resource_metadata="https://solid-trace.example.ts.net/.well-known/oauth-protected-resource/mcp")
    get "/.well-known/oauth-protected-resource/mcp"
    assert_equal props["mcp_url"], response.parsed_body["resource"]
  ensure
    ENV.delete("CONSOLE_URL")
  end

  test "Account no longer lists the tokens" do
    ApiToken.issue!(user: @user, name: "Laptop")

    [ user_settings_url, user_url(org_slug: @organization.slug) ].each do |url|
      get url, headers: inertia_headers
      assert_equal "Settings/User", response.parsed_body["component"]
      assert_not response.parsed_body["props"].key?("api_tokens"), url
    end
  end

  test "making a token answers its secret once and keeps only the digest" do
    assert_difference -> { @user.api_tokens.count }, 1 do
      post api_tokens_url, params: { api_token: { name: "Laptop" } }, as: :json
    end

    assert_response :created
    secret = response.parsed_body["secret"]
    token = @user.api_tokens.last
    assert_match(/\Ast_/, secret)
    assert_equal token, ApiToken.find_by_secret(secret)
    assert_equal({ "id" => token.id, "name" => "Laptop", "created_at" => token.created_at.iso8601, "last_used_at" => nil,
                   "scopes" => [], "organization" => nil, "limits" => {}, "expires_at" => nil, "dispatcher" => false },
      response.parsed_body["api_token"])

    get tokens_page_url, headers: inertia_headers
    assert_not_includes response.body, secret
    assert_not_includes response.body, token.token_digest
  end

  test "a token needs a name" do
    assert_no_difference -> { ApiToken.count } do
      post api_tokens_url, params: { api_token: { name: "" } }, as: :json
    end

    assert_response :unprocessable_content
    assert_includes response.parsed_body.dig("errors", "name"), "can't be blank"
  end

  test "revoking a token deletes its row and goes back to the page" do
    mine, secret = ApiToken.issue!(user: @user, name: "Laptop")

    delete api_token_url(mine), headers: { "Referer" => tokens_page_url }

    assert_redirected_to tokens_page_url
    assert_nil ApiToken.find_by_secret(secret)
  end

  test "revoking with no page to go back to lands on the Organization's API tokens page" do
    mine, = ApiToken.issue!(user: @user, name: "Laptop")

    delete api_token_url(mine)

    assert_redirected_to tokens_page_url
  end

  test "another user's token can't be revoked" do
    delete api_token_url(@others_token)

    assert_response :not_found
    assert ApiToken.exists?(@others_token.id)
  end

  test "signed out, no token is made" do
    delete session_url

    assert_no_difference -> { ApiToken.count } do
      post api_tokens_url, params: { api_token: { name: "Laptop" } }, as: :json
    end
  end

  private

  def tokens_page_url
    api_token_settings_url(org_slug: @organization.slug)
  end

  def inertia_headers
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end

  def inertia_props(url)
    get url, headers: inertia_headers
    assert_response :success
    assert_equal "Settings/ApiTokens", response.parsed_body["component"]
    response.parsed_body["props"]
  end
end
