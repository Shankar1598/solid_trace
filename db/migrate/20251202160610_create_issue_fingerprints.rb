# frozen_string_literal: true

class CreateIssueFingerprints < ActiveRecord::Migration[8.1]
  def change
    create_table :event_fingerprints do |t|
      t.string :fingerprint
      t.references :issue, null: false, foreign_key: true
      t.references :project, null: false, foreign_key: true

      t.timestamps
    end
    add_index :event_fingerprints, [ :project_id, :fingerprint ], unique: true
  end
end
