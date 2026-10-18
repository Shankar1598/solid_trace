# frozen_string_literal: true

module McpTools
  # A table EventStore answered, as a tool answers it: a header of its
  # columns, at most MAX_ROWS of its rows, and how many more there are. A
  # long cell is cut, as a Log's line cuts its message, so grouping by
  # message stays a small answer.
  module Table
    MAX_ROWS = 100
    CELL_CHARS = LogLine::MESSAGE_CHARS
    # What a table answer holds, for the tools' descriptions.
    SHAPE = "its columns and at most #{MAX_ROWS} rows, a long value cut at #{CELL_CHARS} characters".freeze
    # What a tool keeps of a column: its name and type, the aggregate it holds
    # and, on a time bin column, the bin's size.
    COLUMN_KEYS = %w[name type agg bin_seconds].freeze

    # {columns:, rows:} and, past MAX_ROWS or when EventStore cut the table,
    # more, saying how many rows were left out.
    def self.compact(table)
      rows = Array(table["rows"])
      answer = { columns: Array(table["columns"]).map { |column| column.slice(*COLUMN_KEYS) },
                 rows: rows.first(MAX_ROWS).map { |row| row.map { |cell| cell_of(cell) } } }
      more = [ rows.size - MAX_ROWS, 0 ].max

      if table["truncated"]
        answer[:more] = "#{more} more, and EventStore cut the table at #{rows.size} rows: narrow the Query."
      elsif more.positive?
        answer[:more] = "#{more} more: narrow the Query."
      end
      answer
    end

    # A string past CELL_CHARS cut with "…", and an array or an object past
    # it as its JSON, cut the same way.
    def self.cell_of(cell)
      case cell
      when String then LogLine.cut(cell, CELL_CHARS)
      when Array, Hash
        json = JSON.generate(cell)
        json.length > CELL_CHARS ? LogLine.cut(json, CELL_CHARS) : cell
      else cell
      end
    end
  end
end
