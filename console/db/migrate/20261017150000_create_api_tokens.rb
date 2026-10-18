# frozen_string_literal: true

# An API token lets a client act as its user through the MCP server. Console
# keeps only the SHA-256 digest of its secret and finds it by that digest.
# Revoking a token deletes its row.
class CreateApiTokens < ActiveRecord::Migration[8.1]
  def change
    create_table :api_tokens do |t|
      t.references :user, null: false, foreign_key: true
      t.string :name, null: false
      t.string :token_digest, null: false, index: { unique: true }
      t.datetime :last_used_at
      t.timestamps
    end
  end
end
