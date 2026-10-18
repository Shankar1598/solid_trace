# frozen_string_literal: true

# An Integration a Monitor tells about what it finds. Both belong to the same
# Organization. The check is here because every way of linking them saves
# one of these rows; a saved Monitor's integrations= writes them at once,
# before the Monitor validates again.
class MonitorIntegration < ApplicationRecord
  belongs_to :monitor, class_name: "LogMonitor", inverse_of: :monitor_integrations
  belongs_to :integration

  validate :integration_of_the_monitors_organization
  # Unticking the Integration on the Monitor's form destroys this row. A
  # deleted Monitor resolves its groups itself, and a deleted Integration
  # can be told nothing, so both delete their rows without this.
  before_destroy { monitor.resolve_groups_for(integration) }

  private

  def integration_of_the_monitors_organization
    return unless monitor && integration

    errors.add(:integration, "must belong to the Monitor's Organization") unless integration.organization_id == monitor.organization_id
  end
end
