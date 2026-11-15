# frozen_string_literal: true

# Child tokens (GLOSSARY: API token): the Dispatcher's token, which has
# tokens:create, makes child tokens for each Execution. parent_id is the
# token that made a child; nil for every other token. EventStore reads it
# for the agent ingest API (ADR 0001).
#
# Revoking the parent deletes its row, and the database deletes each child
# with it, so a child never outlives its parent.
class AddParentToApiTokens < ActiveRecord::Migration[8.1]
  def change
    add_reference :api_tokens, :parent, foreign_key: { to_table: :api_tokens, on_delete: :cascade }
  end
end
