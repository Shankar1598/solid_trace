# frozen_string_literal: true

require "test_helper"

# Seam 2: the MCP server with an API token that has Scopes, one
# Organization or an expiry, as an agent sends it. A token with no Scope
# keeps today's meaning (mcp_test.rb).
class McpTokenScopesTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PasskeyTestHelper
  include OauthTestHelper

  setup do
    @user = create(:user, name: "Ada", email: "ada@example.com")
    @acme = create(:organization, name: "Acme", slug: "acme")
    @beta = create(:organization, name: "Beta", slug: "beta")
    create(:organization_user, organization: @acme, user: @user, role: :admin)
    create(:organization_user, organization: @beta, user: @user, role: :member)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
  end

  # Expiry, revocation and membership: 401

  test "a token works until its expiry, and gets 401 from that time on" do
    expires_at = 1.hour.from_now.change(usec: 0)
    _, secret = issue(scopes: [ "mcp" ], expires_at: expires_at)

    travel_to(expires_at - 1.second) do
      mcp "tools/list", token: secret
      assert_response :ok
    end
    travel_to(expires_at) do
      mcp "tools/list", token: secret
      assert_response :unauthorized
      assert_equal "Bearer", response.headers["WWW-Authenticate"]
    end
  end

  test "an expired token with no Scope gets 401 too" do
    token, secret = ApiToken.issue!(user: @user, name: "Laptop", expires_at: 1.minute.from_now)

    travel 2.minutes do
      mcp "tools/list", token: secret
      assert_response :unauthorized
    end
    assert_nil token.reload.last_used_at
  end

  test "a revoked token gets 401" do
    token, secret = issue(scopes: [ "mcp:read" ])
    token.destroy!

    mcp "tools/list", token: secret

    assert_response :unauthorized
  end

  test "a token whose user left its Organization gets 401, also if the user is added again later to another" do
    _, secret = issue(scopes: [ "mcp" ])
    @user.organization_users.find_by!(organization: @acme).discard!

    mcp "tools/list", token: secret
    assert_response :unauthorized

    open_console_at
    mcp "tools/list", token: secret
    assert_response :unauthorized
    assert_match 'error="invalid_token"', response.headers["WWW-Authenticate"]
  end

  # An invite brings back the same membership row (MemberInvite), so the
  # token must not come back with it.
  test "a token whose user left its Organization gets 401, also after the user is added again to it" do
    _, secret = issue(scopes: [ "mcp" ])
    membership = @user.organization_users.find_by!(organization: @acme)
    membership.discard!
    membership.undiscard!

    mcp "tools/list", token: secret
    assert_response :unauthorized
  end

  test "a token with no Organization keeps working when its user leaves one of theirs" do
    _, secret = ApiToken.issue!(user: @user, name: "Laptop")
    @user.organization_users.find_by!(organization: @acme).discard!

    mcp "tools/call", token: secret, name: "whoami", arguments: {}

    assert_response :ok
    assert_equal [ "beta" ], JSON.parse(tool_text.split("\nConsole: ").first)["organizations"].pluck("slug")
  end

  # Scopes: 403

  test "a token with Scopes but without mcp or mcp:read gets 403 insufficient_scope" do
    [ [ "factory" ], [ "sessions:write", "run_events:write", "issues:comment" ] ].each do |scopes|
      _, secret = issue(scopes: scopes)

      mcp "tools/list", token: secret

      assert_response :forbidden
      assert_equal %(Bearer error="insufficient_scope", scope="mcp"), response.headers["WWW-Authenticate"]
      assert_match "needs the mcp or mcp:read scope", response.parsed_body["error"]
    end
  end

  test "with OAuth on, the 403 points to the protected resource metadata" do
    open_console_at
    _, secret = issue(scopes: [ "factory" ])

    mcp "tools/list", token: secret

    assert_response :forbidden
    assert_equal %(Bearer error="insufficient_scope", scope="mcp", ) +
                 %(resource_metadata="#{ISSUER}/.well-known/oauth-protected-resource/mcp"),
                 response.headers["WWW-Authenticate"]
  end

  test "a token with mcp in one Organization acts only in it" do
    _, secret = issue(scopes: [ "mcp", "factory" ])

    mcp "tools/call", token: secret, name: "whoami", arguments: {}
    assert_equal [ "acme" ], JSON.parse(tool_text.split("\nConsole: ").first)["organizations"].pluck("slug")

    mcp "tools/call", token: secret, name: "list_dashboards", arguments: { organization: "beta" }
    assert response.parsed_body.dig("result", "isError")
    assert_match "This sign-in is for acme", tool_text
  end

  # mcp:read

  test "with mcp:read, tools/list leaves out each write tool" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    _, secret = issue(scopes: [ "mcp:read" ])

    mcp "tools/list", token: secret

    assert_response :ok
    tools = response.parsed_body.dig("result", "tools")
    names = tools.pluck("name")
    assert_includes names, "whoami"
    assert_includes names, "query_logs"
    assert_includes names, "get_issue"
    %w[create_dashboard update_issue add_comment rotate_key delete_monitor run_monitor invite_member].each do |name|
      assert_not_includes names, name
    end
    assert tools.all? { |tool| tool.dig("annotations", "readOnlyHint") == true }
  end

  test "with mcp:read, a call of a write tool gets 403 insufficient_scope and changes nothing" do
    _, secret = issue(scopes: [ "mcp:read" ])

    assert_no_difference -> { Dashboard.count } do
      mcp "tools/call", token: secret, name: "create_dashboard", arguments: { organization: "acme", name: "Mine" }
    end

    assert_response :forbidden
    assert_equal %(Bearer error="insufficient_scope", scope="mcp"), response.headers["WWW-Authenticate"]
    assert_match "create_dashboard changes things", response.parsed_body["error"]
  end

  test "with mcp:read, a batch that holds a write tool call gets 403" do
    _, secret = issue(scopes: [ "mcp:read" ])
    batch = [
      { jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "whoami", arguments: {} } },
      { jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "create_dashboard", arguments: { name: "Mine" } } }
    ]

    assert_no_difference -> { Dashboard.count } do
      post mcp_url, params: batch.to_json, headers: headers_for(secret)
    end

    assert_response :forbidden
  end

  test "with mcp:read, a read tool answers" do
    _, secret = issue(scopes: [ "mcp:read" ])

    mcp "tools/call", token: secret, name: "list_dashboards", arguments: {}

    assert_response :ok
    assert_not response.parsed_body.dig("result", "isError"), tool_text
  end

  test "with mcp:read, list_runs and get_run answer from the Agent Project's run events" do
    agent = create(:project, organization: @acme, name: "Agent sessions", slug: "agent-sessions", agent: true)
    run = { id: "r1", seq: 1, time: "2026-11-15T10:00:00Z", kind: "created", state: "queued", project: "shop",
            actor: { kind: "person", id: "7", name: "Ana" }, pr: { url: "", outcome: "" }, executions: 0, active: true }
    stub_request(:post, "#{EventStore.base_url}/api/#{agent.id}/runs").to_return(status: 200, body: { runs: [ run ], more: false }.to_json)
    stub_request(:get, "#{EventStore.base_url}/api/#{agent.id}/runs/r1").to_return(status: 200, body: { run: run, events: [] }.to_json)
    _, secret = issue(scopes: [ "mcp:read" ])

    mcp "tools/call", token: secret, name: "list_runs", arguments: {}
    assert_response :ok
    assert_not response.parsed_body.dig("result", "isError"), tool_text
    assert_match '"id":"r1"', tool_text

    mcp "tools/call", token: secret, name: "get_run", arguments: { run: "r1" }
    assert_response :ok
    assert_not response.parsed_body.dig("result", "isError"), tool_text
    assert_match '"state":"queued"', tool_text
  end

  test "with mcp:read, tools/list shows list_agent_sessions and get_agent_session, and both answer" do
    agent = create(:project, organization: @acme, name: "Agent sessions", slug: "agent-sessions", agent: true)
    id = "0b6c2f1e-6a51-4c8e-9a3d-0f6d1c2b3a41"
    row = { id: id, first: "2026-11-15T10:00:00Z", last: "2026-11-15T10:05:00Z", user_id: @user.id, run_id: "", project: "",
            repository: "github.com/acme/shop", flags: [] }
    stub_request(:post, "#{EventStore.base_url}/api/#{agent.id}/agent-sessions")
      .to_return(status: 200, body: { agent_sessions: [ row ], totals: { agent_sessions: 1 }, more: false, repositories: [] }.to_json)
    stub_request(:post, "#{EventStore.base_url}/api/#{agent.id}/agent-sessions/#{id}")
      .to_return(status: 200, body: { agent_session: row, subagents: [], steps: [], next_cursor: "" }.to_json)
    _, secret = issue(scopes: [ "mcp:read" ])

    mcp "tools/list", token: secret
    tools = response.parsed_body.dig("result", "tools").index_by { |tool| tool["name"] }
    %w[list_agent_sessions get_agent_session].each do |name|
      assert_match "Agent session", tools.fetch(name)["description"]
    end

    mcp "tools/call", token: secret, name: "list_agent_sessions", arguments: {}
    assert_not response.parsed_body.dig("result", "isError"), tool_text
    assert_match %("id":"#{id}"), tool_text

    mcp "tools/call", token: secret, name: "get_agent_session", arguments: { agent_session: id }
    assert_not response.parsed_body.dig("result", "isError"), tool_text
    assert_match %("name":"Ada"), tool_text
  end

  test "mcp holds mcp:read: a token with both can call a write tool" do
    _, secret = issue(scopes: [ "mcp", "mcp:read" ])

    assert_difference -> { Dashboard.count }, 1 do
      mcp "tools/call", token: secret, name: "create_dashboard", arguments: { name: "Mine" }
    end
    assert_not response.parsed_body.dig("result", "isError"), tool_text
  end

  # mcp:read with a Project limit (an Issue Run's token): the server reads
  # only that Project.

  test "a Project-limited token's server has only the tools that read inside one Project" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)

    mcp "tools/list", token: secret

    assert_response :ok
    names = response.parsed_body.dig("result", "tools").pluck("name")
    assert_equal %w[query_logs get_log list_fields query_language_reference run_sql list_transactions get_transaction
                    get_log_transform preview_log_transform list_issues get_issue], names
  end

  test "a Project-limited token's server says which Project it reads" do
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)

    mcp "initialize", token: secret, protocolVersion: "2025-06-18", capabilities: {}, clientInfo: { name: "test", version: "1" }

    assert_response :ok
    assert_match "only the Project shop in acme", response.parsed_body.dig("result", "instructions")
  end

  test "a Project-limited token reads that Project's Logs, with no organization argument" do
    run = stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query")
      .to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)

    mcp "tools/call", token: secret, name: "query_logs", arguments: { project: "shop", query: "where level == \"error\"" }

    assert_response :ok
    assert_not response.parsed_body.dig("result", "isError"), tool_text
    assert_match "Logs of shop", tool_text
    assert_requested run
  end

  test "a Project-limited token reads that Project's Issues" do
    stub_request(:get, %r{/api/#{@shop.id}/events}).to_raise(Errno::ECONNREFUSED)
    mine = create(:issue, project: @shop, title: "Payment failed")
    create(:issue, project: create(:project, organization: @acme, slug: "admin"), title: "Other Project's Issue")
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)

    mcp "tools/call", token: secret, name: "get_issue", arguments: { project: "shop", issue: mine.number }
    assert_not response.parsed_body.dig("result", "isError"), tool_text
    assert_match "Payment failed", tool_text

    mcp "tools/call", token: secret, name: "list_issues", arguments: {}
    assert_not response.parsed_body.dig("result", "isError"), tool_text
    assert_match "Payment failed", tool_text
    assert_no_match "Other Project's Issue", tool_text
  end

  test "a Project-limited token gets an error for another Project, and EventStore is not asked" do
    other = create(:project, organization: @acme, name: "Admin", slug: "admin")
    create(:issue, project: other, title: "Other Project's Issue")
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)

    mcp "tools/call", token: secret, name: "query_logs", arguments: { project: "admin", query: "" }
    assert response.parsed_body.dig("result", "isError")
    assert_match "This token reads only the Project shop", tool_text

    mcp "tools/call", token: secret, name: "list_issues", arguments: { project: "admin" }
    assert response.parsed_body.dig("result", "isError")
    assert_no_match "Other Project's Issue", tool_text

    mcp "tools/call", token: secret, name: "get_issue", arguments: { project: "admin", issue: 1 }
    assert response.parsed_body.dig("result", "isError")
    assert_match "This token reads only the Project shop", tool_text
    assert_not_requested :any, %r{/api/#{other.id}/}
  end

  test "a Project-limited token gets an error for the Agent Project" do
    agent = create(:project, organization: @acme, name: "Agent sessions", slug: "agent-sessions", agent: true)
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)

    mcp "tools/call", token: secret, name: "query_logs", arguments: { project: "agent-sessions", query: "" }
    assert response.parsed_body.dig("result", "isError")
    assert_match "This token reads only the Project shop", tool_text

    [ [ "list_agent_sessions", {} ], [ "list_runs", {} ], [ "get_run", { run: "r1" } ] ].each do |name, arguments|
      mcp "tools/call", token: secret, name: name, arguments: arguments
      assert response.parsed_body["error"] || response.parsed_body.dig("result", "isError"), name
    end
    assert_not_requested :any, %r{/api/#{agent.id}/}
  end

  test "a Project-limited token gets an error for a Project of another Organization of its user" do
    create(:project, organization: @beta, name: "Shop", slug: "shop")
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)

    mcp "tools/call", token: secret, name: "query_logs", arguments: { organization: "beta", project: "shop", query: "" }

    assert response.parsed_body.dig("result", "isError")
    assert_match "This sign-in is for acme", tool_text
  end

  test "a Project-limited token cannot call a tool that reads the Organization beyond its Project" do
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)
    create(:project, organization: @acme, name: "Admin", slug: "admin")

    %w[list_projects whoami get_overview list_dashboards get_trace get_project_setup].each do |name|
      mcp "tools/call", token: secret, name: name, arguments: { project: "shop", trace_id: "abc" }
      assert response.parsed_body["error"] || response.parsed_body.dig("result", "isError"), name
      assert_no_match "admin", response.body, name
    end
  end

  test "a Project-limited token cannot call a write tool" do
    target = create(:issue, project: @shop)
    _, secret = issue(scopes: [ "mcp:read" ], project_id: @shop.id)

    assert_no_difference -> { Comment.count } do
      mcp "tools/call", token: secret, name: "add_comment", arguments: { project: "shop", issue: target.number, body: "Hi" }
    end
    assert_response :forbidden
    assert_match "add_comment changes things", response.parsed_body["error"]

    mcp "tools/call", token: secret, name: "update_issue", arguments: { project: "shop", issue: target.number, status: "resolved" }
    assert_response :forbidden
    assert target.reload.unresolved?
  end

  test "a child token of the Dispatcher's token, limited to one Project, reads only that Project" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query")
      .to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)
    create(:project, organization: @acme, name: "Admin", slug: "admin")
    parent, = issue(scopes: [ "run_events:write", "issues:comment", "tokens:create" ], expires_at: 30.days.from_now)
    _, secret = ApiToken.issue_child!(parent: parent, name: "Run r1", scopes: [ "mcp:read" ], expires_at: 1.hour.from_now,
                                      project_id: @shop.id)

    mcp "tools/call", token: secret, name: "query_logs", arguments: { project: "shop", query: "" }
    assert_not response.parsed_body.dig("result", "isError"), tool_text

    mcp "tools/call", token: secret, name: "query_logs", arguments: { project: "admin", query: "" }
    assert response.parsed_body.dig("result", "isError")
    assert_match "This token reads only the Project shop", tool_text
  end

  test "an OAuth token with only mcp:read gets the read tools only" do
    open_console_at
    client = Oauth::Client.find_by!(client_id: registered_client)
    grant = Oauth::Grant.create!(user: @user, client: client, organization: @acme, scopes: [ "mcp:read" ],
                                 resources: [ RESOURCE ], approved_via: "browser", approved_at: Time.current)
    secret = Oauth::Token.issue_pair!(grant: grant, resource: RESOURCE)[:access_token]

    mcp "tools/list", token: secret
    assert_response :ok
    assert_not_includes response.parsed_body.dig("result", "tools").pluck("name"), "create_dashboard"

    mcp "tools/call", token: secret, name: "create_dashboard", arguments: { name: "Mine" }
    assert_response :forbidden
  end

  test "every tool on the server says whether it reads or writes" do
    McpServer::TOOLS.each do |tool|
      assert_not_nil tool.annotations_value, "#{tool.name_value} has no read_only!, writes! or destructive!"
    end
  end

  private

  def issue(scopes:, organization_id: @acme.id, **fields)
    ApiToken.issue!(user: @user, name: "Token", scopes: scopes, organization_id: organization_id, **fields)
  end

  def tool_text
    response.parsed_body.dig("result", "content").map { |content| content["text"] }.join("\n")
  end

  def headers_for(secret)
    { "Accept" => "application/json, text/event-stream", "Content-Type" => "application/json",
      "Authorization" => "Bearer #{secret}" }
  end

  def mcp(method, token:, **params)
    post mcp_url, params: { jsonrpc: "2.0", id: 1, method: method, params: params }.to_json, headers: headers_for(token)
  end
end
