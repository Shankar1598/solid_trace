# frozen_string_literal: true

# Where CallTimings writes its lines: stdout in production, which Vector ships
# to the Meta Organization's Project (docs/adr/0011), and log/call_timings.log
# in development. Tests write none unless a test sets it.
#
# Each line also says which release wrote it, the one the image was built
# from (console/Dockerfile), or dev.
Rails.application.config.x.solidtrace_version = ENV.fetch("SOLIDTRACE_VERSION", "dev")

Rails.application.config.x.call_timings_output =
  case Rails.env
  when "production" then $stdout.tap { |io| io.sync = true }
  when "development" then File.open(Rails.root.join("log/call_timings.log"), "a").tap { |io| io.sync = true }
  end
