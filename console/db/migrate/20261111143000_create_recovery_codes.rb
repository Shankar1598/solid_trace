# frozen_string_literal: true

# A user's Recovery codes: each one's digest only (the codes are shown once,
# when made), and when it was used, as each works once.
class CreateRecoveryCodes < ActiveRecord::Migration[8.1]
  def change
    create_table :recovery_codes do |t|
      t.references :user, null: false, foreign_key: true
      t.string :digest, null: false
      t.datetime :used_at
      t.timestamps
    end
  end
end
