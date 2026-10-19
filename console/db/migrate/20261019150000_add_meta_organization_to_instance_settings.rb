# frozen_string_literal: true

# Which Organization is the Meta Organization (ADR 0011), and whether it was
# seeded, so a deleted one stays deleted. EventStore reads other columns of
# instance_settings by name, so new columns don't concern it (ADR 0001).
class AddMetaOrganizationToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :meta_organization_seeded, :boolean, default: false, null: false
    add_reference :instance_settings, :meta_organization, foreign_key: { to_table: :organizations, on_delete: :nullify }
  end
end
