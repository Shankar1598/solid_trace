# frozen_string_literal: true

# When Console made the Organization's starter 'Factory' Dashboard
# (FactoryDashboard). It is set one time, so a member who deletes the
# Dashboard does not get a new one.
class AddFactoryDashboardMadeAtToOrganizations < ActiveRecord::Migration[8.1]
  def change
    add_column :organizations, :factory_dashboard_made_at, :datetime
  end
end
