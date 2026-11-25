# frozen_string_literal: true

# What Console sends its own Sentry, the Meta Project on an install
# (config/initializers/sentry.rb, docs/traces.md).
#
# - Its Issues, while the instance setting Send Console's Issues is on. A job
#   that notifies the Meta Organization's Integrations, about its Issues or its
#   Monitors, sends no Event when it fails, though: a Console Issue is a Meta
#   Project Issue, so its Event would notify those Integrations again, fail
#   again, and loop. ActiveJob still logs the failure.
# - Its Transactions, while Trace Console is on: the share
#   SENTRY_TRACES_SAMPLE_RATE gives, but never of the every-second message job
#   or of health checks.
#
# Both settings apply with no restart. They're read once every SETTINGS_FOR,
# not for each Event or Transaction, and at once by the process that saves
# them.
module ConsoleSentry
  # The context on Sentry's scope that marks a job notifying the Meta
  # Organization. sentry-rails gives each job a scope of its own.
  CONTEXT = "console_sentry"

  # Requests never traced: health checks, which deploy/install and uptime
  # monitors poll.
  HEALTH_CHECK_PATHS = %w[/up /health].freeze

  # Jobs never traced: EventStoreMessageProcessorJob runs every second.
  UNTRACED_JOBS = %w[EventStoreMessageProcessorJob].freeze

  # How long a read of the settings is kept.
  SETTINGS_FOR = 10.seconds

  # The settings ConsoleSentry reads, and what it assumes when it can't.
  DEFAULTS = { send_console_issues: true, trace_console: true, meta_organization_id: nil }.freeze

  class << self
    # Called by a notification job with the Organization it notifies, before
    # anything that can fail.
    def notifying(organization_id)
      return unless organization_id && organization_id == settings[:meta_organization_id]

      Sentry.set_context(CONTEXT, { notifies_meta_organization: true })
    end

    # Whether the frontend sends its errors: Console's Sentry has a DSN it
    # sends to, and Send Console's Issues is on.
    def browser_errors?
      Sentry.initialized? && Sentry.configuration.sending_allowed? && settings[:send_console_issues]
    end

    # Sends a browser's Sentry envelope, its frames un-minified
    # (BrowserSourceMaps), through Console's own transport, which puts
    # Console's DSN key on it. false when the frontend sends no errors. Raises
    # Sentry::ExternalError when the send fails.
    def send_browser_envelope(body)
      return false unless browser_errors?

      Sentry.get_current_client.transport.send_data(BrowserSourceMaps.rewrite_envelope(body))
      true
    end

    # Sentry's before_send: the Event, or nil to drop it.
    def before_send(event, _hint)
      return unless settings[:send_console_issues]
      return if event.contexts.dig(CONTEXT, :notifies_meta_organization)

      event
    end

    # Sentry's traces_sampler, with the rate from SENTRY_TRACES_SAMPLE_RATE:
    # the share of this request's or job's Transactions to keep. Otherwise a
    # trace sampled, or not, upstream keeps that decision.
    def traces_sample_rate(sampling_context, rate)
      return 0.0 if !settings[:trace_console] || noise?(sampling_context)
      return sampling_context[:parent_sampled] unless sampling_context[:parent_sampled].nil?

      rate
    end

    # Forgets the settings read, so the next Event or Transaction reads them.
    def reset
      @settings = nil
    end

    private

    def noise?(sampling_context)
      env = sampling_context[:env]
      return HEALTH_CHECK_PATHS.include?(env["PATH_INFO"]) if env

      UNTRACED_JOBS.include?(sampling_context.dig(:transaction_context, :name))
    end

    # The settings, read again once SETTINGS_FOR has passed. Sentry calls
    # before_send on its own thread, so the read is swapped in whole.
    def settings
      read = @settings
      return read[:values] if read && Time.current < read[:until]

      values = read_settings
      @settings = { values: values, until: Time.current + SETTINGS_FOR }
      values
    end

    # While the database can't be read, Console sends as it did before these
    # settings.
    def read_settings
      InstanceSetting.current.slice(*DEFAULTS.keys).symbolize_keys
    rescue ActiveRecord::ActiveRecordError => e
      Rails.logger.warn("ConsoleSentry couldn't read the instance settings: #{e.message}")
      DEFAULTS
    end
  end
end
