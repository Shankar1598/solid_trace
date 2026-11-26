# frozen_string_literal: true

# Evidence files: the screenshots and videos that an agent uploads to prove a
# PR, one row for each file. EventStore writes the rows and keeps the bytes
# (ADR 0001: event_store/storage/evidence_files.go writes this table, so
# change both in the same commit). The id is a random UUID, so a link cannot
# be guessed. agent_session_id has no foreign key: a file can arrive before
# its Agent session. A second upload of the same bytes to the same Agent
# session gives the same row, while it is not deleted. Evidence Retention
# sets deleted_at and keeps the row, so that its link can say when the file
# was deleted; an upload of the same bytes after that gets a new row.
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
