# frozen_string_literal: true

# The operator's commands for the instance settings (InstanceSetting). There is
# no UI for them: Console has no instance-admin role.
namespace :instance_settings do
  print_settings = lambda do |settings|
    puts "SQL queries: #{settings.sql_queries_enabled? ? "on" : "off"}"
    puts "Default Log Retention: #{settings.default_log_retention_days} days"
    puts
    puts InstanceSetting::SQL_QUERIES_DESCRIPTION
  end

  desc "Show the instance settings and what turning SQL queries on allows"
  task show: :environment do
    print_settings.call(InstanceSetting.current)
  end

  desc "Change the instance settings: SQL_QUERIES_ENABLED=true|false and/or DEFAULT_LOG_RETENTION_DAYS=<whole days>. " \
    "Read what SQL queries allow first (instance_settings:show)"
  task set: :environment do
    changes = {}

    if ENV.key?("SQL_QUERIES_ENABLED")
      value = ENV["SQL_QUERIES_ENABLED"].to_s.strip.downcase
      changes[:sql_queries_enabled] =
        case value
        when "true", "on" then true
        when "false", "off" then false
        else abort "SQL_QUERIES_ENABLED must be true or false, not #{ENV["SQL_QUERIES_ENABLED"].inspect}"
        end
    end

    if ENV.key?("DEFAULT_LOG_RETENTION_DAYS")
      changes[:default_log_retention_days] = ENV["DEFAULT_LOG_RETENTION_DAYS"]
    end

    abort "Nothing to change. Give SQL_QUERIES_ENABLED=true|false and/or DEFAULT_LOG_RETENTION_DAYS=<whole days>." if changes.empty?

    settings = InstanceSetting.current
    abort "Not changed: #{settings.errors.full_messages.to_sentence}" unless settings.update(changes)

    print_settings.call(settings)
  end
end
