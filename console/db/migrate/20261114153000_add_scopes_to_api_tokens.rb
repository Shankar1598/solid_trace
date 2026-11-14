# frozen_string_literal: true

# API tokens get Scopes, limits, one Organization and an expiry (GLOSSARY:
# API token). A token with no Scope keeps its old meaning: it acts as its
# user over the MCP server, in each of the user's Organizations. EventStore
# reads these columns for the agent ingest API (ADR 0001: EventStore reads
# Console's tables).
#
# - scopes: the names of its Scopes (Oauth::Scopes); [] is no Scope.
# - organization_id: the one Organization it acts in; nil acts in each.
# - expires_at: nil does not expire.
# - agent_session_ids: the Agent session ids that sessions:write and
#   sessions:read are limited to; nil is no list.
# - project_id: the one Project that mcp:read is limited to; nil is no limit.
#
# Deleting the Organization or the Project deletes the token, so a limit
# never points to nothing.
class AddScopesToApiTokens < ActiveRecord::Migration[8.1]
  def change
    add_column :api_tokens, :scopes, :json, null: false, default: []
    add_reference :api_tokens, :organization, foreign_key: { on_delete: :cascade }
    add_column :api_tokens, :expires_at, :datetime
    add_column :api_tokens, :agent_session_ids, :json
    add_reference :api_tokens, :project, foreign_key: { on_delete: :cascade }
  end
end
