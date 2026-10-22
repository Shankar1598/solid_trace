# frozen_string_literal: true

# The SMTP server Console sends email through, and the sender, which instance
# admins set on the Email settings page instead of SMTP_* env vars (ADR 0015).
# smtp_password holds Active Record Encryption's ciphertext. EventStore reads
# other columns of instance_settings by name, so it doesn't concern it (ADR 0001).
class AddEmailSettingsToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :smtp_address, :string
    add_column :instance_settings, :smtp_port, :integer, default: 587, null: false
    add_column :instance_settings, :smtp_username, :string
    add_column :instance_settings, :smtp_password, :text
    add_column :instance_settings, :mail_from, :string
  end
end
