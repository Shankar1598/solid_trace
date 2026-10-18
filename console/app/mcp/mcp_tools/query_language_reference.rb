# frozen_string_literal: true

module McpTools
  # The Query language's reference, docs/logs/query-language.md. The Console
  # image is built from console/ alone, so it reads the copy in
  # app/mcp/docs, which a test keeps the same.
  class QueryLanguageReference < Base
    DOC = Rails.root.join("app/mcp/docs/query-language.md")

    description "Answers the whole reference of the Query language query_logs, Charts and Monitors run: its stages, " \
                "operators, aggregates and functions, how Fields' types are matched, and how a table is charted."
    read_only!

    def run
      text(DOC.read)
    end
  end
end
