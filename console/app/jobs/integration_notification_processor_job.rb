# frozen_string_literal: true

# The per-Integration delivery tick. At most one runs at a time per Integration.
class IntegrationNotificationProcessorJob < ApplicationJob
  queue_as :default

  limits_concurrency to: 1,
    key: ->(integration_id, *_args) { integration_id },
    duration: 2.minutes,
    on_conflict: :discard

  def perform(integration_id)
    ConsoleSentry.notifying(Integration.where(id: integration_id).pick(:organization_id))
    IntegrationNotification.deliver_pending(integration_id)
  end
end
