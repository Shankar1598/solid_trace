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

    # Forgets the settings read, so the next Event or Transaction reads them.
    def reset
      @settings = nil
    end

    private

    def settings
      @settings ||= { meta_organization_id: InstanceSetting.current.meta_organization_id }
    end
  end
end
