# frozen_string_literal: true

module McpTools
  # Changes the install's settings that apply at once, as Settings → Instance
  # does. For instance admins. Sign-up and the settings read at start can't
  # change here.
  class UpdateInstanceSettings < Base
    include InstanceSettingsTool

    description "Changes the install's settings that apply at once, with no restart. Only the settings given " \
                "change. Sign-up and the settings that need a restart can't change here: Sign-up changes only from " \
                "a shell on the server, so a stolen session or API token can't open the install. Answers the " \
                "settings as get_instance_settings does. Only an instance admin can."
    writes!
    argument :sql_queries_enabled, { type: "boolean", description: "true turns SQL queries on. #{InstanceSetting::SQL_QUERIES_DESCRIPTION.squish}" }
    argument :default_log_retention_days, { type: "integer",
      description: "Whole days that Projects with no Retention of their own keep Logs, at least 1. EventStore's next " \
                   "Retention run deletes older Logs." }
    argument :log_all_reads, { type: "boolean", description: "true times every Log read, the MCP server's included, to investigate slowness." }
    argument :log_mcp_reads, { type: "boolean",
      description: "true times the MCP server's Log reads, the default. It applies only while log_all_reads is off, " \
                   "as log_all_reads times them too; false still saves then, for when log_all_reads is turned off." }

    def run
      require_instance_admin!("change the instance settings")
      settings = InstanceSetting.current
      raise ToolError, "Not saved: #{settings.errors.full_messages.join('; ')}." unless settings.update_editable(arguments)

      settings_answer(settings)
    end
  end
end
