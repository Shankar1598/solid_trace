# frozen_string_literal: true

# A user's Passkeys (ADR 0018): each one's credential id and public key, its
# sign count, the name the user gave it, the host it was made for (the
# relying-party ID, the Console URL's host then), and when it was last used.
# Users gain a WebAuthn user handle, made with their first Passkey.
class CreatePasskeys < ActiveRecord::Migration[8.1]
  def change
    create_table :passkeys do |t|
      t.references :user, null: false, foreign_key: true
      t.string :credential_id, null: false, index: { unique: true }
      t.string :public_key, null: false
      t.bigint :sign_count, null: false, default: 0
      t.string :name, null: false
      t.string :host, null: false
      t.datetime :last_used_at
      t.timestamps
    end

    add_column :users, :webauthn_id, :string
    add_index :users, :webauthn_id, unique: true
  end
end
