# frozen_string_literal: true

# The install's settings as Settings → Instance and the MCP server show them,
# in three sections:
#
# - editable: the settings that apply at once, which an instance admin changes
#   (InstanceSetting#update_editable).
# - sign_up: whether Sign-up is open. It's read-only: it changes only from a
#   shell on the server, so a stolen instance admin session or API token can't
#   open the install to anyone.
# - restart_to_change: groups of settings read at start, each
#   { name:, settings: [{ label:, source:, value:, default:, secret: }] }, or
#   { name:, unavailable: reason } when they can't be read. A secret's value
#   is only whether it's set.
class InstanceSettingsAnswer
  SIGN_UP_COMMAND = "bin/rails instance_settings:set SIGN_UP_OPEN=true|false"

  # console_url is the URL the request reached Console at.
  def initialize(settings = InstanceSetting.current, console_url:, env: ENV)
    @settings = settings
    @console_url = console_url
    @env = env
  end

  def as_json(*)
    {
      editable: {
        sql_queries_enabled: @settings.sql_queries_enabled,
        sql_queries_description: InstanceSetting::SQL_QUERIES_DESCRIPTION,
        default_log_retention_days: @settings.default_log_retention_days,
        default_trace_retention_days: @settings.default_trace_retention_days,
        log_all_reads: @settings.log_all_reads,
        log_mcp_reads: @settings.log_mcp_reads,
      },
      sign_up: {
        open: @settings.sign_up_open,
        command: SIGN_UP_COMMAND,
      },
      restart_to_change: [
        { name: "Console", settings: ConsoleEnvironment.new(console_url: @console_url, env: @env).settings },
        event_store_group
      ],
    }
  end

  private

  # EventStore's config as it loaded it at start, or why it can't be read.
  def event_store_group
    { name: "EventStore", settings: EventStoreConfig.new(EventStore.config).settings }
  rescue EventStore::Unavailable => e
    { name: "EventStore", unavailable: "Couldn't read EventStore's config: #{e.message}" }
  end
end
