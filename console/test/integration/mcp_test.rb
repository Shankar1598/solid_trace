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
    InstanceSetting.current.update!(sql_queries_enabled: false)

    mcp "tools/list"
    names = response.parsed_body.dig("result", "tools").map { |tool| tool["name"] }
    assert_includes names, "whoami"
    assert_includes names, "list_projects"
    assert_not_includes names, "run_sql"
    assert_empty names & sql_tools

    InstanceSetting.current.update!(sql_queries_enabled: true)
    mcp "tools/list"
    names = response.parsed_body.dig("result", "tools").map { |tool| tool["name"] }
    assert_includes names, "run_sql"
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
    assert_equal({ "name" => "Ada", "email" => "ada@example.com", "instance_admin" => false }, answer["user"])
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

  test "a member's rotate_key and invite_member are refused, and an admin's work" do
    key = @project.project_keys.create!
    member = create(:user)
    create(:organization_user, organization: @organization, user: member, role: :member)
    _, member_secret = ApiToken.issue!(user: member, name: "Laptop")

    mcp "tools/call", token: member_secret, name: "rotate_key", arguments: { project: "shop", key: key.id }
    assert_equal [ true, "Only an Organization admin can rotate a Project's keys." ], tool_result
    mcp "tools/call", token: member_secret, name: "invite_member", arguments: { email: "grace@example.com" }
    assert_equal [ true, "Only an Organization admin can invite members." ], tool_result

    old = key.public_key
    mcp "tools/call", name: "rotate_key", arguments: { project: "shop", key: key.id }
    assert_not tool_result.first
    assert_not_equal old, key.reload.public_key
    mcp "tools/call", name: "invite_member", arguments: { email: "grace@example.com" }
    assert_not tool_result.first
    assert @organization.users.exists?(email: "grace@example.com")
  end

  test "a body that isn't JSON gets JSON-RPC's parse error" do
    post mcp_url, params: "{not json", headers: { "Authorization" => "Bearer #{@secret}",
                                                  "Accept" => "application/json, text/event-stream",
                                                  "Content-Type" => "application/json" }

    assert_response :bad_request
    assert_equal(-32700, response.parsed_body.dig("error", "code"))
  end

  test "GET and DELETE answer 405, as the stateless transport has no stream or session" do
    get mcp_url, headers: { "Authorization" => "Bearer #{@secret}" }
    assert_response :method_not_allowed

    delete mcp_url, headers: { "Authorization" => "Bearer #{@secret}" }
    assert_response :method_not_allowed
  end

  private

  # A tools/call's isError and text.
  def tool_result
    result = response.parsed_body["result"]
    [ result["isError"] || false, result["content"].map { |content| content["text"] }.join("\n") ]
  end

  def mcp(method, token: @secret, **params)
    headers = { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json" }
    headers["Authorization"] = "Bearer #{token}" if token
    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: method, params: params }.to_json, headers: headers
  end
end
