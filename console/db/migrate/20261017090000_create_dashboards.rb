# frozen_string_literal: true

# Dashboards belong to an Organization; each chart runs a saved Query on a
# Project of that Organization. default_range is a span back from now, as the
# explorer's links hold it (`1h`, `24h`, `7d`).
class CreateDashboards < ActiveRecord::Migration[8.1]
  def change
    create_table :dashboards do |t|
      t.references :organization, null: false, foreign_key: true
      t.string :name, null: false
      t.text :description
      t.string :default_range, null: false, default: "1h"
      t.string :auto_refresh, null: false, default: "off"
      t.references :created_by, foreign_key: { to_table: :users, on_delete: :nullify }
      t.timestamps
    end

    create_table :dashboard_charts do |t|
      t.references :dashboard, null: false, foreign_key: true
      t.references :project, null: false, foreign_key: true
      t.string :title, null: false
      t.text :query, null: false
      t.string :viz, null: false, default: "line"
      t.json :options, null: false, default: {}
      t.integer :x, null: false, default: 0
      t.integer :y, null: false, default: 0
      t.integer :w, null: false, default: 6
      t.integer :h, null: false, default: 4
      t.timestamps
    end
  end
end
