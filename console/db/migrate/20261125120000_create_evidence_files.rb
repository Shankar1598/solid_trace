# frozen_string_literal: true

# EventStore writes the rows and keeps the bytes (ADR 0001:
# event_store/storage/evidence_files.go writes this table, so change both in
# the same commit).
class CreateEvidenceFiles < ActiveRecord::Migration[8.1]
  def change
    create_table :evidence_files, id: :string do |t|
      t.references :organization, null: false, foreign_key: true, index: false
      t.string :agent_session_id, null: false
      t.string :sha256, null: false
      t.string :caption
      t.string :content_type, null: false
      t.integer :byte_size, null: false
      t.string :storage_key, null: false
      t.references :uploaded_by_user, foreign_key: { to_table: :users, on_delete: :nullify }, index: false
      t.datetime :created_at, null: false
      t.datetime :deleted_at
    end

    add_index :evidence_files, %i[organization_id agent_session_id sha256], unique: true,
      where: "deleted_at IS NULL", name: "index_evidence_files_on_session_and_sha256"
    add_index :evidence_files, %i[organization_id created_at]
  end
end
