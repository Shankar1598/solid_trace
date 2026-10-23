# frozen_string_literal: true

# The MCP server one request talks to (McpController): Console's tools, acting
# as the user whose API token the request presents.
module McpServer
  # Every tool, in the order tools/list gives them. A tool is a class under
  # McpTools (see McpTools::Base), and listing it here puts it on the server.
  TOOLS = [
    McpTools::Whoami,
    McpTools::ListProjects,
    McpTools::QueryLogs,
    McpTools::GetLog,
    McpTools::ListFields,
    McpTools::QueryLanguageReference,
    McpTools::RunSql,
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
    McpTools::UpdateIssue,
    McpTools::AddComment,
    McpTools::CreateProject,
    McpTools::GetProjectSetup,
    McpTools::CreateKey,
    McpTools::RotateKey,
    McpTools::DeleteKey,
    McpTools::ListMembers,
    McpTools::InviteMember,
    McpTools::GetEmailSettings,
    McpTools::UpdateEmailSettings,
    McpTools::SendTestEmail,
    McpTools::GetInstanceSettings
  ].freeze

  INSTRUCTIONS = <<~TEXT
    SolidTrace keeps Projects' Logs and Issues, and Organizations' Dashboards and Monitors.
    You act as the user whose API token you sent, with their role in each Organization.
    Tools take an Organization's slug, which you can leave out when you belong to one, and a Project's slug.
    whoami lists your Organizations, and list_projects an Organization's Projects.
    query_logs runs a Query on a Project's Logs; list_fields lists the names a Query reads, and query_language_reference explains the language.
    To read less, filter with == or in on service and file, which every Project's Logs are sorted by; query_language_reference's Reading less says how.
    get_log_transform shows the program that shapes a Project's Logs before they're stored.
    Dashboards, Monitors and Issues each have list_ and get_ tools, and tools to make and change them; get_issue's trace_logs finds an Event's Logs.
    An instance admin (whoami says) reads and changes the install's email settings with get_email_settings and update_email_settings.
    An answer ends with a link to the Console page that shows it, for the user to open.
  TEXT

  # A server for one request, with the tools listed now: one that needs SQL
  # queries only while they're on.
  def self.build(user:, console_url:)
    MCP::Server.new(
      name: "solidtrace",
      title: "SolidTrace",
      instructions: INSTRUCTIONS,
      tools: TOOLS.select(&:listed?),
      server_context: { user: user, console_url: console_url },
    )
  end
end
