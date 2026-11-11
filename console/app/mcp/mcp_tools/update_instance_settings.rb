# frozen_string_literal: true

module McpTools
  # Changes the install's settings that apply at once (InstanceSetting::EDITABLE),
  # as Settings → Instance does. For instance admins. Sign-up and the settings read at start can't
  # change here. A Console URL on another host that strands Passkeys needs
  # confirm_passkeys_stop_working, so an agent can't break them by accident.
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
    argument :default_trace_retention_days, { type: "integer",
      description: "Whole days that Projects with no trace Retention of their own keep Spans, at least 1. EventStore's " \
                   "next Retention run deletes older Spans." }
    argument :log_all_reads, { type: "boolean", description: "true times every Log read, the MCP server's included, to investigate slowness." }
    argument :log_mcp_reads, { type: "boolean",
      description: "true times the MCP server's Log reads, the default. It applies only while log_all_reads is off, " \
                   "as log_all_reads times them too; false still saves then, for when log_all_reads is turned off." }
    argument :send_console_issues, { type: "boolean",
      description: "true, the default, sends Console's own Issues to its Sentry DSN, the Meta Project on an install. " \
                   "false sends none, from the next Event on." }
    argument :trace_console, { type: "boolean",
      description: "true, the default, traces the share of Console's requests and jobs SENTRY_TRACES_SAMPLE_RATE " \
                   "gives into its Sentry DSN, the Meta Project on an install. false traces none, from the next " \
                   "request or job on." }
    argument :logs_text_match_budget_mb, { type: "integer",
      description: "The most a Query that matches text (search, contains, startswith or =~ on message, " \
                   "or a function of message) may read, " \
                   "in MiB of kept bytes over its range and the files its filters keep, before the Logs explorer and " \
                   "query_logs refuse it with narrower ways to run it. Dashboard Charts and Monitors are never " \
                   "refused. 0 refuses none. Default 4096. It applies from the next Query." }
    argument :logs_read_threads, { type: "integer",
      description: "The threads ClickHouse may use for each Log read (Queries, tables, histograms, Fields, a Log " \
                   "by id, Match monitor runs, traces and Transactions; not SQL queries), 0 to 64. More threads read " \
                   "the bucket faster but use more CPU, which ingest shares. 0, the default, is ClickHouse's own " \
                   "default of one per CPU core. It applies from the next read." }
    argument :local_cache_hours, { type: "integer",
      description: "Hours that Logs and Spans stay on local disk before their parts move to the bucket, 0 to 720; " \
                   "0 moves each part once stored. Default 24. It does nothing without a bucket (restart_to_change's " \
                   "EventStore group names it), and applies within a minute. Before changing it, tell the user: " \
                   "#{InstanceSetting::LOCAL_CACHE_HOURS_WARNING}" }
    argument :console_url, { type: "string",
      description: "Console's URL, http(s)://host[:port] with no path, which emails, invite links, the MCP URL and " \
                   "these answers' links use from now on. Its scheme must match the HTTPS mode Console started in " \
                   "(restart_to_change's HTTPS row), which changes only with CONSOLE_URL in deploy/.env and a " \
                   "restart. An empty string clears it, falling back to CONSOLE_URL, then the address the request " \
                   "reached Console at (editable.console_url_fallback)." }
    argument :ingest_url, { type: "string",
      description: "Ingest's URL as SDKs and shippers reach it, http(s)://host[:port][/path], which DSNs, the Logs " \
                   "snippets and get_project_setup use from now on; DSNs already in apps don't change. An empty " \
                   "string clears it, falling back to PUBLIC_INGEST_URL, then Console's host at port 4000 " \
                   "(editable.ingest_url_fallback)." }
    argument :confirm_passkeys_stop_working, { type: "boolean",
      description: "true changes console_url to another host even though Passkeys made for the current host would " \
                   "stop working; their users then sign in with another factor or a Recovery code. Without it, " \
                   "such a change is refused with the count of Passkeys and users. Tell the user the counts and " \
                   "set it only once they agree." }

    def run
      require_instance_admin!("change the instance settings")
      settings = InstanceSetting.current
      unless settings.update_editable(arguments, confirm_passkeys_stop_working: arguments[:confirm_passkeys_stop_working] == true)
        if settings.errors.of_kind?(:base, :passkeys_stop_working)
          raise ToolError, "Not saved: #{settings.errors[:base].first} Tell the user, and call again with " \
                           "confirm_passkeys_stop_working: true to change it anyway."
        end
        raise ToolError, "Not saved: #{settings.errors.full_messages.join('; ')}."
      end

      settings_answer(settings)
    end
  end
end
