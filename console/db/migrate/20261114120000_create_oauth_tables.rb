# frozen_string_literal: true

# OAuth for the MCP server: the OAuth clients, the Grants that users approve,
# the authorization codes, the OAuth tokens (access and refresh), and the
# approval page's pending requests. Codes and tokens are kept only as
# SHA-256 digests, as API tokens are.
class CreateOauthTables < ActiveRecord::Migration[8.1]
  def change
    # A registered (DCR) client. client_id is what the client sends.
    create_table :oauth_clients do |t|
      t.string :kind, null: false
      t.string :client_id, null: false, index: { unique: true }
      t.string :name, null: false
      t.json :redirect_uris, null: false, default: []
      t.json :grant_types, null: false, default: []
      t.string :token_endpoint_auth_method, null: false, default: "none"
      t.string :client_secret_digest
      t.json :metadata, null: false, default: {}
      t.timestamps
    end

    # One approval by a user of a client, for one Organization.
    create_table :oauth_grants do |t|
      t.references :user, null: false, foreign_key: true
      t.references :oauth_client, null: false, foreign_key: true
      t.references :organization, null: false, foreign_key: true
      t.json :scopes, null: false, default: []
      t.json :resources, null: false, default: []
      t.string :approved_via, null: false
      t.string :user_agent
      t.string :ip_address
      t.datetime :approved_at, null: false
      t.datetime :first_token_at
      t.datetime :last_used_at
      t.datetime :revoked_at
      t.timestamps
    end

    create_table :oauth_codes do |t|
      t.string :kind, null: false
      t.string :code_digest, null: false, index: { unique: true }
      t.references :oauth_client, null: false, foreign_key: true
      t.references :oauth_grant, foreign_key: true
      t.string :code_challenge
      t.string :redirect_uri
      t.json :scopes, null: false, default: []
      t.string :resource
      t.datetime :expires_at, null: false
      t.datetime :used_at
      t.timestamps
    end

    create_table :oauth_tokens do |t|
      t.string :kind, null: false
      t.string :token_digest, null: false, index: { unique: true }
      t.references :oauth_grant, null: false, foreign_key: true
      t.string :resource
      t.datetime :expires_at, null: false
      t.datetime :used_at
      t.datetime :revoked_at
      t.timestamps
    end

    # A request to the approval page that Console checked, kept for 10
    # minutes under a random key. The Rails session holds only the key.
    create_table :oauth_pending_requests do |t|
      t.string :key, null: false, index: { unique: true }
      t.references :oauth_client, null: false, foreign_key: true
      t.string :redirect_uri, null: false
      t.string :response_type
      t.string :code_challenge
      t.string :code_challenge_method
      t.string :scope
      t.string :resource
      t.text :state
      t.timestamps
    end
  end
end
