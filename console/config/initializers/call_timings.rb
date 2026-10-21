# frozen_string_literal: true

# Where CallTimings writes its lines: stdout in production, which Vector ships
# to the Meta Organization's Project (docs/adr/0011), and log/call_timings.log
# in development. Tests write none unless a test sets it.
#
# Each line also says which SolidTrace wrote it: the release and commit the
# image was built from (console/Dockerfile), or, in development, dev and the
# checkout's commit.
Rails.application.config.x.solidtrace_version = ENV.fetch("SOLIDTRACE_VERSION", "dev")
Rails.application.config.x.solidtrace_revision = ENV["SOLIDTRACE_REVISION"].presence ||
  (Rails.env.development? ? `git -C #{Rails.root} rev-parse HEAD 2>/dev/null`.strip.presence : nil)

Rails.application.config.x.call_timings_output =
  case Rails.env
  when "production" then $stdout.tap { |io| io.sync = true }
  when "development" then File.open(Rails.root.join("log/call_timings.log"), "a").tap { |io| io.sync = true }
  end
