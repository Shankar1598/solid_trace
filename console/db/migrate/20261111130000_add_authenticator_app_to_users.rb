# frozen_string_literal: true

# A user's Authenticator app: its secret (encrypted, ADR 0015), when a code
# from it confirmed it (until then it is being set up and isn't Two-factor),
# and the last time step a code was accepted for, so no step works twice.
class AddAuthenticatorAppToUsers < ActiveRecord::Migration[8.1]
  def change
    add_column :users, :authenticator_secret, :string
    add_column :users, :authenticator_confirmed_at, :datetime
    add_column :users, :authenticator_last_step, :integer
  end
end
