# frozen_string_literal: true

# The MCP server one request talks to (McpController): Console's tools, acting
# as the user whose API token the request presents.
module McpServer
  # Every tool, in the order tools/list gives them. A tool is a class under
  # McpTools (see McpTools::Base), and listing it here puts it on the server.
  TOOLS = [
    McpTools::Whoami,
    McpTools::ListProjects,
    McpTools::GetOverview,
    McpTools::QueryLogs,
    McpTools::GetLog,
    McpTools::ListFields,
    McpTools::QueryLanguageReference,
    McpTools::RunSql,
    McpTools::ListTransactions,
    McpTools::GetTransaction,
    McpTools::GetLogTransform,
    McpTools::PreviewLogTransform,
    McpTools::UpdateLogTransform,
    McpTools::ListDashboards,
    McpTools::GetDashboard,
    McpTools::CreateDashboard,
    McpTools::UpdateDashboard,
    McpTools::DeleteDashboard,
    McpTools::AddChart,
    McpTools::UpdateChart,
    McpTools::DeleteChart,
    McpTools::GetChartData,
    McpTools::ListMonitors,
    McpTools::GetMonitor,
    McpTools::CreateMonitor,
    McpTools::UpdateMonitor,
    McpTools::DeleteMonitor,
    McpTools::RunMonitor,
    McpTools::ListIntegrations,
    McpTools::ListIssues,
    McpTools::GetIssue,
    McpTools::GetTrace,
    McpTools::ListAgentSessions,
    McpTools::GetAgentSession,
    McpTools::ListRuns,
    McpTools::GetRun,
    McpTools::UpdateIssue,
    McpTools::AddComment,
    McpTools::CreateProject,
    McpTools::UpdateProject,
    McpTools::GetProjectSetup,
    McpTools::CreateKey,
    McpTools::RotateKey,
    McpTools::DeleteKey,
    McpTools::ListMembers,
    McpTools::InviteMember,
    McpTools::GetEmailSettings,
    McpTools::UpdateEmailSettings,
    McpTools::SendTestEmail,
    McpTools::GetInstanceSettings,
    McpTools::UpdateInstanceSettings
  ].freeze

  INSTRUCTIONS = <<~TEXT
    SolidTrace keeps Projects' Logs and Issues, and Organizations' Dashboards and Monitors.
    You act as the user whose API token you sent, with their role in each Organization.
    whoami lists your Organizations, and list_projects an Organization's Projects, with descriptions that say which to query.
    get_overview answers how much an Organization's Projects sent and store, how fast, how healthy, and what that volume would cost elsewhere.
    query_logs runs a Query on a Project's Logs; list_fields lists the names a Query reads, and query_language_reference explains the language.
    list_transactions lists a Project's traced requests and jobs by the time they take, and get_transaction finds where one's time goes.
    get_issue's trace_logs finds an Event's Logs, and get_trace a trace's Spans, Issues and Logs across the Organization's Projects.
    list_agent_sessions lists the recorded Agent sessions of coding agents, and get_agent_session reads one's timeline in pages, or searches it.
    list_runs lists the factory's Runs, and get_run reads one Run's Executions, report and events.
    An answer ends with a link to the Console page that shows it, for the user to open.
  TEXT

  # A server for one request, with the tools listed now: one that needs SQL
  # queries only while they're on. request_url is the address the request
  # reached Console at, which the tools' links fall back to (PublicUrls).
  #
  # organization is an OAuth token's one Organization: the tools then act
  # only in it, and the install's settings tools are not on the server, as
  # the user approved acting in one Organization only.
  #
  # read_only is for a token with mcp:read and not mcp: the server then has
  # only the read tools, so no write tool can run.
  def self.build(user:, request_url:, organization: nil, read_only: false)
    tools = TOOLS.select(&:listed?)
    tools = tools.reject(&:instance_admin_only?) if organization
    tools = tools.select(&:read_only?) if read_only
    MCP::Server.new(
      name: "solidtrace",
      title: "SolidTrace",
      instructions: INSTRUCTIONS,
      tools: tools,
      server_context: { user: user, request_url: request_url, organization: organization },
    )
  end
end
