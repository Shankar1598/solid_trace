# frozen_string_literal: true

# Require two-factor (GLOSSARY): an Organization that admits only members
# with Two-factor to its pages and MCP calls. Off until an admin turns it on.
class AddRequireTwoFactorToOrganizations < ActiveRecord::Migration[8.1]
  def change
    add_column :organizations, :require_two_factor, :boolean, null: false, default: false
  end
end
