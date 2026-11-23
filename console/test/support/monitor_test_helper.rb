# frozen_string_literal: true

require "webmock/minitest"

# Stubs EventStore as a Monitor's Query check on save sees it: validate finds
# no mistakes, places the last stage at the Query's last line and says the
# Query names the Dashboard filters +parameters+, a Threshold monitor's dry
# run answers a table of +columns+, and arming a Match monitor answers
# +ingest_cursor+. Tests that run Monitors stub logs/query again after
# saving, so their own answers win.
module MonitorTestHelper
  NIL_CURSOR = "00000000-0000-0000-0000-000000000000"
  COUNT_COLUMN = { "name" => "count_", "type" => "number", "agg" => "count" }.freeze
  SERVICE_COLUMN = { "name" => "service", "type" => "string" }.freeze

  def stub_monitor_query_check(project, columns: [ COUNT_COLUMN ], ingest_cursor: NIL_CURSOR, parameters: [])
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/validate").to_return do |request|
      { status: 200, body: { errors: [], stages: [ last_stage(JSON.parse(request.body)["query"]) ], parameters: parameters }.to_json }
    end
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/query")
      .with(body: hash_including("from", "to"))
      .to_return(status: 200, body: { table: { columns: columns, rows: [] } }.to_json)
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/query")
      .with(body: hash_including("after_batch" => ""))
      .to_return(status: 200, body: { logs: [], total: 0, ingest_cursor: ingest_cursor, next_cursor: nil }.to_json)
  end

  # Turns SQL queries on and answers a SQL Monitor's save check, its dry run
  # on EventStore's SQL endpoint, with a table of columns and no rows.
  def stub_monitor_sql_check(project, columns: [ SERVICE_COLUMN, COUNT_COLUMN ])
    InstanceSetting.current.update!(sql_queries_enabled: true)
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/sql")
      .to_return(status: 200, body: { table: { columns: columns, rows: [], truncated: false } }.to_json)
  end

  private

  # Where the stage on the Query's last line starts, after its pipe.
  def last_stage(query)
    lines = query.split("\n")
    column = lines.last.index(/[^|\s]/).to_i + 1
    { "line" => lines.size, "column" => column, "end_line" => lines.size, "end_column" => lines.last.length + 1 }
  end
end
