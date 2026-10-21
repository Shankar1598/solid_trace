# frozen_string_literal: true

# Where CallTimings writes its lines: stdout in production, which Vector ships
# to the Meta Organization's Project (docs/adr/0011), and log/call_timings.log
# in development. Tests write none unless a test sets it.
Rails.application.config.x.call_timings_output =
  case Rails.env
  when "production" then $stdout.tap { |io| io.sync = true }
  when "development" then File.open(Rails.root.join("log/call_timings.log"), "a").tap { |io| io.sync = true }
  end
