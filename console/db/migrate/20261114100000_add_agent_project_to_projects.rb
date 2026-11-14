# frozen_string_literal: true

# The Agent Project (GLOSSARY): one Project of each Organization that holds
# its Agent sessions, made when an Admin turns on Agent sessions. Its
# captured repositories are its setting, which the agent ingest API reads
# (ADR 0001: EventStore reads Console's tables). Each confirm of the capture
# notice is recorded: who, when, and for which entries.
class AddAgentProjectToProjects < ActiveRecord::Migration[8.1]
  def change
    add_column :projects, :agent, :boolean, null: false, default: false
    add_column :projects, :captured_repositories, :json, null: false, default: []
    # One Agent Project for each Organization, also under two turn-ons at once.
    add_index :projects, :organization_id, unique: true, where: "agent", name: "index_projects_one_agent_project"

    create_table :capture_confirmations do |t|
      t.references :project, null: false, foreign_key: { on_delete: :cascade }
      t.references :user, foreign_key: { on_delete: :nullify }
      t.json :entries, null: false, default: []
      t.datetime :created_at, null: false
    end
  end
end
