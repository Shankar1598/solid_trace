# frozen_string_literal: true

module McpTools
  # A table EventStore answered, as a tool answers it: a header of its
  # columns, at most MAX_ROWS of its rows, and how many more there are.
  module Table
    MAX_ROWS = 100
    # What a tool keeps of a column: its name and type, the aggregate it holds
    # and, on a time bin column, the bin's size.
    COLUMN_KEYS = %w[name type agg bin_seconds].freeze

    # {columns:, rows:} and, past MAX_ROWS or when EventStore cut the table,
    # more, saying how many rows were left out.
    def self.compact(table)
      rows = Array(table["rows"])
      answer = { columns: Array(table["columns"]).map { |column| column.slice(*COLUMN_KEYS) }, rows: rows.first(MAX_ROWS) }
      more = [ rows.size - MAX_ROWS, 0 ].max

      if table["truncated"]
        answer[:more] = "#{more} more, and EventStore cut the table at #{rows.size} rows: narrow the Query."
      elsif more.positive?
        answer[:more] = "#{more} more: narrow the Query."
      end
      answer
    end
  end
end
