# frozen_string_literal: true

require "test_helper"

# A table EventStore answered, as the MCP server's tools answer it.
class McpTableTest < ActiveSupport::TestCase
  COLUMNS = [ { "name" => "service", "type" => "string", "extra" => 1 }, { "name" => "count_", "type" => "number", "agg" => "count" } ].freeze

  test "keeps each column's name, type and aggregate, and every row of a short table" do
    table = McpTools::Table.compact({ "columns" => COLUMNS, "rows" => [ [ "web", 3 ] ], "truncated" => false })

    assert_equal({ columns: [ { "name" => "service", "type" => "string" }, { "name" => "count_", "type" => "number", "agg" => "count" } ],
                   rows: [ [ "web", 3 ] ] }, table)
  end

  test "answers at most 100 rows, saying how many more there are and whether EventStore cut the table" do
    rows = Array.new(10_000) { |i| [ "service-#{i}", i ] }

    assert_equal "9900 more: narrow the Query.", McpTools::Table.compact({ "columns" => COLUMNS, "rows" => rows })[:more]

    cut = McpTools::Table.compact({ "columns" => COLUMNS, "rows" => rows, "truncated" => true })
    assert_equal 100, cut[:rows].size
    assert_equal "9900 more, and EventStore cut the table at 10000 rows: narrow the Query.", cut[:more]
  end
end
