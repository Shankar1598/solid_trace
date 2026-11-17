# frozen_string_literal: true

# Sections (GLOSSARY): titled groups of a Dashboard's Charts, in an order. A
# Chart with no section_id is in no Section. When a Section is deleted, its
# Charts stay on the Dashboard with no Section.
class CreateDashboardSections < ActiveRecord::Migration[8.1]
  def change
    create_table :dashboard_sections do |t|
      t.references :dashboard, null: false, foreign_key: true
      t.string :title, null: false
      t.integer :position, null: false, default: 0
      t.timestamps
    end

    add_reference :dashboard_charts, :section, foreign_key: { to_table: :dashboard_sections, on_delete: :nullify }
  end
end
