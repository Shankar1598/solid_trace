# frozen_string_literal: true

# The MCP server one request talks to (McpController): Console's tools, acting
# as the user whose API token the request presents.
module McpServer
  # Every tool, in the order tools/list gives them. A tool is a class under
  # McpTools (see McpTools::Base), and listing it here puts it on the server.
  TOOLS = [
    McpTools::Whoami,
    McpTools::ListProjects,
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
    McpTools::ListIntegrations
  ].freeze

  INSTRUCTIONS = <<~TEXT
    SolidTrace keeps Projects' Logs and Issues, and Organizations' Dashboards and Monitors.
    You act as the user whose API token you sent, with their role in each Organization.
    Tools take an Organization's slug, which you can leave out when you belong to one, and a Project's slug.
    whoami lists your Organizations, and list_projects an Organization's Projects.
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
