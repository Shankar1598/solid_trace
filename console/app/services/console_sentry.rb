# frozen_string_literal: true

# What Console sends its own Sentry, the Meta Project on an install
# (config/initializers/sentry.rb, docs/traces.md).
#
# A job that notifies the Meta Organization's Integrations, about its Issues or
# its Monitors, sends no Event when it fails: a Console Issue is a Meta Project
# Issue, so its Event would notify those Integrations again, fail again, and
# loop. ActiveJob still logs the failure.
module ConsoleSentry
  # The context on Sentry's scope that marks a job notifying the Meta
  # Organization. sentry-rails gives each job a scope of its own.
  CONTEXT = "console_sentry"

  # Requests never traced: health checks, which deploy/install and uptime
  # monitors poll.
  HEALTH_CHECK_PATHS = %w[/up /health].freeze

  # Jobs never traced: EventStoreMessageProcessorJob runs every second.
  UNTRACED_JOBS = %w[EventStoreMessageProcessorJob].freeze

  class << self
    # Called by a notification job with the Organization it notifies, before
    # anything that can fail.
    def notifying(organization_id)
      return unless organization_id && organization_id == settings[:meta_organization_id]

      Sentry.set_context(CONTEXT, { notifies_meta_organization: true })
    end

    # Sentry's before_send: the Event, or nil to drop it.
    def before_send(event, _hint)
      return if event.contexts.dig(CONTEXT, :notifies_meta_organization)

      event
    end

    # Sentry's traces_sampler, with the rate from SENTRY_TRACES_SAMPLE_RATE:
    # the share of this request's or job's Transactions to keep. The message
    # job that runs every second and health checks are never kept. Otherwise
    # a trace sampled, or not, upstream keeps that decision.
    def traces_sample_rate(sampling_context, rate)
      return 0.0 if noise?(sampling_context)
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

    def settings
      @settings ||= { meta_organization_id: InstanceSetting.current.meta_organization_id }
    end
  end
end
