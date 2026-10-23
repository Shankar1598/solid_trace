# frozen_string_literal: true

# A Dashboard's filters: an ordered JSON array of {name, label, kind,
# multiple, options}, the controls of its filter bar, which Charts name in
# their Queries as $name (.scratch/dashboard-filters/spec.md, ADR 0014).
class AddFiltersToDashboards < ActiveRecord::Migration[8.1]
  def change
    add_column :dashboards, :filters, :json, null: false, default: []
  end
end
