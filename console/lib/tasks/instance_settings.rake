# frozen_string_literal: true

# The operator's commands for the instance settings (InstanceSetting). There is
# no UI for them: Console has no instance-admin role.
namespace :instance_settings do
  print_settings = lambda do |settings|
    puts "SQL queries: #{settings.sql_queries_enabled? ? "on" : "off"}"
    puts "Sign-up: #{settings.sign_up_open? ? "open" : "closed: people join by Invite, but the first user can always sign up"}"
    puts "Default Log Retention: #{settings.default_log_retention_days} days"
    puts "Log all reads: #{settings.log_all_reads? ? "on: every Log read and its SQL is timed into the Meta Organization's Project" : "off: only the MCP server's"}"
    puts
    puts InstanceSetting::SQL_QUERIES_DESCRIPTION
  end

  desc "Show the instance settings and what turning SQL queries on allows"
  task show: :environment do
    print_settings.call(InstanceSetting.current)
  end

  desc "Change the instance settings: SQL_QUERIES_ENABLED=true|false, SIGN_UP_OPEN=true|false, LOG_ALL_READS=true|false " \
    "and/or DEFAULT_LOG_RETENTION_DAYS=<whole days>. Read what SQL queries allow first (instance_settings:show)"
  task set: :environment do
    changes = {}

    boolean = lambda do |name|
      case ENV[name].to_s.strip.downcase
      when "true", "on" then true
      when "false", "off" then false
      else abort "#{name} must be true or false, not #{ENV[name].inspect}"
      end
    end

    changes[:sql_queries_enabled] = boolean.call("SQL_QUERIES_ENABLED") if ENV.key?("SQL_QUERIES_ENABLED")
    changes[:sign_up_open] = boolean.call("SIGN_UP_OPEN") if ENV.key?("SIGN_UP_OPEN")
    changes[:log_all_reads] = boolean.call("LOG_ALL_READS") if ENV.key?("LOG_ALL_READS")

    if ENV.key?("DEFAULT_LOG_RETENTION_DAYS")
      changes[:default_log_retention_days] = ENV["DEFAULT_LOG_RETENTION_DAYS"]
    end

    abort "Nothing to change. Give SQL_QUERIES_ENABLED=true|false, SIGN_UP_OPEN=true|false, LOG_ALL_READS=true|false " \
      "and/or DEFAULT_LOG_RETENTION_DAYS=<whole days>." if changes.empty?

    settings = InstanceSetting.current
    abort "Not changed: #{settings.errors.full_messages.to_sentence}" unless settings.update(changes)

    print_settings.call(settings)
  end
end
