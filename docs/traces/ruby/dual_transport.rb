require "sentry-ruby"

# Sends each envelope to SolidTrace as well as to the DSN in Sentry's config,
# sentry.io's: the same bytes, each with its own key in the auth header, which
# is all SolidTrace finds the Project by. SOLIDTRACE_DSN is the Project's DSN
# in SolidTrace; unset, only sentry.io gets them. When SolidTrace fails, the
# SDK logs a warning, and sentry.io still gets the envelope.
class DualTransport < Sentry::HTTPTransport
  def initialize(configuration, dsn: nil)
    super(configuration)
    if dsn
      # The copy that sends to SolidTrace.
      @dsn = Sentry::DSN.new(dsn)
    elsif ENV["SOLIDTRACE_DSN"].to_s != ""
      @solidtrace = DualTransport.new(configuration, dsn: ENV["SOLIDTRACE_DSN"])
    end
  end

  def send_data(data, *)
    super(data)
  ensure
    begin
      @solidtrace&.send_data(data)
    rescue Sentry::ExternalError => e
      log_warn("SolidTrace didn't take the envelope: #{e.message}")
    end
  end
end
