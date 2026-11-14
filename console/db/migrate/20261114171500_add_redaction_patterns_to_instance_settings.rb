# frozen_string_literal: true

# The extra Redaction patterns that an Instance admin adds on Settings →
# Instance, after EventStore's default list (event_store/redaction). Each is a
# Go regular expression that EventStore checked when it was saved. EventStore
# reads this column on each upload of a unit of an Agent session's file, and
# replaces each byte that a pattern matches with '*' before it stores the unit.
class AddRedactionPatternsToInstanceSettings < ActiveRecord::Migration[8.1]
  def change
    add_column :instance_settings, :redaction_patterns, :json, null: false, default: []
  end
end
