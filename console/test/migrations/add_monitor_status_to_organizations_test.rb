# frozen_string_literal: true

require "test_helper"

# SQLite makes a table again to add a foreign key or a check constraint, and
# the drop of the old table can delete the rows of the tables that point at
# it. The migrations of the Monitor status run as `db:migrate` runs them, on
# a database of their own with one row in every table, and keep every row.
class AddMonitorStatusToOrganizationsTest < ActiveSupport::TestCase
  self.use_transactional_tests = false

  BEFORE = 20261126100000
  AFTER = 20261126180000
  NEW_COLUMNS = {
    "organizations" => %w[monitor_status_integration_id],
    "monitors" => %w[not_running_since not_running_told_integration_id held_since],
    "monitor_runs" => %w[held_reason],
  }.freeze
  # Values that a check constraint needs.
  ROWS = { "notifications" => { "monitor_id" => nil, "group_key" => nil } }.freeze

  test "the migrations up and down keep every row of every table" do
    with_own_database do |connection|
      ActiveRecord::Migration.suppress_messages { load Rails.root.join("db/schema.rb") }
      migrate(:down, BEFORE)
      fill(connection)
      before = rows(connection)

      migrate(:up, AFTER)
      assert_equal before, rows(connection)
      assert_empty connection.select_rows("PRAGMA foreign_key_check")

      NEW_COLUMNS.each { |table, columns| connection.execute("UPDATE #{table} SET #{columns.map { |c| "#{c} = 1" }.join(', ')}") }
      migrate(:down, BEFORE)
      assert_equal before, rows(connection)
    end
  end

  private

  def with_own_database
    Dir.mktmpdir do |dir|
      original = ActiveRecord::Base.connection_db_config
      config = ActiveRecord::DatabaseConfigurations::HashConfig.new("test", "primary", adapter: "sqlite3", database: File.join(dir, "migrate.sqlite3"))
      ActiveRecord::Base.establish_connection(config)
      yield ActiveRecord::Base.lease_connection
    ensure
      ActiveRecord::Base.establish_connection(original)
    end
  end

  def migrate(direction, version)
    ActiveRecord::Migration.suppress_messages do
      ActiveRecord::Base.connection_pool.migration_context.public_send(direction, version)
    end
  end

  def tables(connection)
    connection.tables - %w[schema_migrations ar_internal_metadata]
  end

  # One row in each table, with every whole number 1. Each table's row has
  # id 1, so each foreign key points at a row.
  def fill(connection)
    connection.disable_referential_integrity do
      tables(connection).each do |table|
        row = connection.columns(table).to_h { |c| [ c.name, value(c) ] }.merge(ROWS.fetch(table, {}))
        connection.execute("INSERT INTO #{table} (#{row.keys.join(', ')}) VALUES (#{row.values.map { |v| connection.quote(v) }.join(', ')})")
      end
    end
    assert_empty connection.select_rows("PRAGMA foreign_key_check")
  end

  def value(column)
    case column.type
    when :integer, :float, :decimal then 1
    when :boolean then false
    when :datetime then "2026-11-26 12:00:00"
    when :date then "2026-11-26"
    when :json then "{}"
    else "x"
    end
  end

  def rows(connection)
    tables(connection).to_h do |table|
      columns = connection.columns(table).map(&:name) - NEW_COLUMNS.fetch(table, [])
      [ table, connection.select_rows("SELECT #{columns.join(', ')} FROM #{table}") ]
    end
  end
end
