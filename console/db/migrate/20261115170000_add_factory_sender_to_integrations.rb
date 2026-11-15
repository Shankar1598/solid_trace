# frozen_string_literal: true

# A 'Factory' Integration keeps the Webhook sender that Console made for it
# at the Dispatcher: the sender's id, and its secret, which Console encrypts
# with keys from secret_key_base (ADR 0015). Each other provider keeps none.
# Settings → Instance gets the Dispatcher's public webhook URL, which only
# the Factory provider uses, where Console has no private path to the
# Dispatcher.
class AddFactorySenderToIntegrations < ActiveRecord::Migration[8.1]
  def change
    add_column :integrations, :sender_id, :string
    add_column :integrations, :sender_secret, :text
    add_column :instance_settings, :dispatcher_webhook_url, :string
  end
end
