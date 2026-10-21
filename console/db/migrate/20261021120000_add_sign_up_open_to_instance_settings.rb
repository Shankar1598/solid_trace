# frozen_string_literal: true

# Whether anyone may sign up once the install has a user. Closed by default,
# so an install facing the internet takes members only by Invite. EventStore
# reads other columns of instance_settings by name, so it doesn't concern it
# (ADR 0001).
class AddSignUpOpenToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :sign_up_open, :boolean, default: false, null: false
  end
end
