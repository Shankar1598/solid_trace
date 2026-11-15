# frozen_string_literal: true

# The Dispatcher's private API URL (Settings → Instance). With none, Console
# shows no part of the Factory that needs the Dispatcher.
class AddDispatcherUrlToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :dispatcher_url, :string
  end
end
