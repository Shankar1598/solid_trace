# frozen_string_literal: true

# What a Project holds, in a sentence or two, so agents over MCP can tell
# which Project to query.
class AddDescriptionToProjects < ActiveRecord::Migration[8.1]
  def change
    add_column :projects, :description, :text
  end
end
