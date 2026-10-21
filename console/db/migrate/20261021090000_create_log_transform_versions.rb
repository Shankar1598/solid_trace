# frozen_string_literal: true

# Every saved version of a Project's Log transform, a VRL program. The newest
# row is the Project's current Log transform; no row, or an empty program,
# means none. Restoring a version inserts a copy of it. EventStore reads this
# table to run the program in Log processing (ADR 0001, ADR 0012), so its Go
# test seeding changes with it.
class CreateLogTransformVersions < ActiveRecord::Migration[8.1]
  def change
    create_table :log_transform_versions do |t|
      t.references :project, null: false, foreign_key: { on_delete: :cascade }, index: false
      t.text :program, null: false, default: ""
      t.references :user, foreign_key: { on_delete: :nullify }
      t.datetime :created_at, null: false
    end
    # The newest row per Project, which EventStore reads on each pass.
    add_index :log_transform_versions, [ :project_id, :id ]
  end
end
