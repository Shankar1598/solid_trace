# frozen_string_literal: true

require "test_helper"

# JSON-RPC to POST /mcp, as Claude Code and Cursor send it with an API token.
class McpTest < ActionDispatch::IntegrationTest
  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @organization = create(:organization, name: "Acme", slug: "acme")
    create(:organization_user, organization: @organization, user: @user, role: :admin)
    @project = create(:project, organization: @organization, name: "Shop", slug: "shop")
    @token, @secret = ApiToken.issue!(user: @user, name: "Laptop")
  end

  test "initialize names the server and offers tools" do
    mcp "initialize", protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "test", version: "1" }

    assert_response :ok
    assert_equal "application/json", response.media_type
    result = response.parsed_body["result"]
    assert_equal "solidtrace", result.dig("serverInfo", "name")
    assert_equal "2025-06-18", result["protocolVersion"]
    assert result["capabilities"].key?("tools")
  end

  test "tools/list holds the SQL tools only while SQL queries are on" do
    sql_tools = McpServer::TOOLS.select(&:needs_sql_queries?).map(&:name_value)

    mcp "tools/list"
    names = response.parsed_body.dig("result", "tools").map { |tool| tool["name"] }
    assert_includes names, "whoami"
    assert_includes names, "list_projects"
    assert_not_includes names, "run_sql"
    assert_empty names & sql_tools

    InstanceSetting.current.update!(sql_queries_enabled: true)
    mcp "tools/list"
    names = response.parsed_body.dig("result", "tools").map { |tool| tool["name"] }
    assert_equal McpServer::TOOLS.map(&:name_value).sort, names.sort
  end

  test "tools/list marks read tools read-only" do
    mcp "tools/list"

    whoami = response.parsed_body.dig("result", "tools").find { |tool| tool["name"] == "whoami" }
    assert_equal true, whoami.dig("annotations", "readOnlyHint")
    assert_equal false, whoami.dig("annotations", "destructiveHint")
  end

  test "tools/call whoami answers the token's user, their Organizations and their role in each" do
    other = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: other, user: @user, role: :member)

    mcp "tools/call", name: "whoami", arguments: {}

    assert_response :ok
    result = response.parsed_body["result"]
    assert_not result["isError"]
    body, link = result["content"].sole["text"].split("\nConsole: ")
    answer = JSON.parse(body)
    assert_equal({ "name" => "Ada", "email" => "ada@example.com" }, answer["user"])
    assert_equal [ { "slug" => "acme", "name" => "Acme", "role" => "admin" },
                   { "slug" => "beta", "name" => "Beta", "role" => "member" } ], answer["organizations"]
    assert_equal "http://www.example.com/user_settings", link
  end

  test "a call records the token's use" do
    mcp "tools/call", name: "whoami", arguments: {}

    assert_not_nil @token.reload.last_used_at
  end

  test "no token, an unknown one or a revoked one gets a 401 that asks for a Bearer token" do
    mcp "tools/list", token: nil
    assert_response :unauthorized
    assert_equal "Bearer", response.headers["WWW-Authenticate"]

    mcp "tools/list", token: "st_unknown"
    assert_response :unauthorized

    @token.destroy!
    mcp "tools/list"
    assert_response :unauthorized
    assert_equal "Bearer", response.headers["WWW-Authenticate"]
  end

  test "another Organization's slug is answered as not found" do
    stranger = create(:user)
    theirs = create(:organization, name: "Theirs", slug: "theirs")
    create(:organization_user, organization: theirs, user: stranger, role: :admin)
    create(:project, organization: theirs, name: "Secret", slug: "secret")

    mcp "tools/call", name: "list_projects", arguments: { organization: "theirs" }

    assert_response :ok
    result = response.parsed_body["result"]
    assert result["isError"]
    text = result["content"].sole["text"]
    assert_match "No Organization theirs", text
    assert_match "acme", text
    assert_no_match "secret", text
  end

  test "GET and DELETE answer 405, as the stateless transport has no stream or session" do
    get mcp_url, headers: { "Authorization" => "Bearer #{@secret}" }
    assert_response :method_not_allowed

    delete mcp_url, headers: { "Authorization" => "Bearer #{@secret}" }
    assert_response :method_not_allowed
  end

  private

  def mcp(method, token: @secret, **params)
    headers = { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json" }
    headers["Authorization"] = "Bearer #{token}" if token
    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: method, params: params }.to_json, headers: headers
  end
end
