# frozen_string_literal: true

# Refresh rotation: a used refresh token keeps the token that replaced it,
# and for 60 seconds the new pair's secrets, encrypted with Active Record
# Encryption, so that a second use in that window gets the same pair again.
class AddRotationToOauthTokens < ActiveRecord::Migration[8.1]
  def change
    add_reference :oauth_tokens, :replaced_by, foreign_key: { to_table: :oauth_tokens, on_delete: :nullify }
    add_column :oauth_tokens, :kept_access_token, :text
    add_column :oauth_tokens, :kept_refresh_token, :text
  end
end
