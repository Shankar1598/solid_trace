class CreateIssueFingerprints < ActiveRecord::Migration[8.1]
  def change
    create_table :issue_fingerprints do |t|
      t.string :fingerprint
      t.references :issue, null: false, foreign_key: true

      t.timestamps
    end
    add_index :issue_fingerprints, [ :issue_id, :fingerprint ], unique: true
  end
end
