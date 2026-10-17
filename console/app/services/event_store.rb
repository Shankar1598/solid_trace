# frozen_string_literal: true

require "rest-client"
require "json"

class EventStore
  # EventStore could not answer: an error response, a connection error, a
  # timeout or an unreadable body. body is the error body EventStore answered
  # with (its JSON, or {"error" => text}), or nil when it didn't answer.
  class Unavailable < StandardError
    attr_reader :body

    def initialize(message = nil, body: nil)
      @body = body
      super(message)
    end
  end

  # EventStore refused a request it could read, such as a Query with
  # mistakes: a 400 whose JSON body says why.
  class Rejected < StandardError
    attr_reader :body

    def initialize(body)
      @body = body
      super("EventStore rejected the request: #{body}")
    end
  end

  # EventStore stopped a run at the timeout_seconds it was given: a 504 whose
  # JSON body says so. An Unavailable, so callers that give no timeout treat
  # it as any other failure.
  class TimedOut < Unavailable
    def initialize(body)
      super("EventStore stopped the run at its timeout: #{body}", body: body)
    end
  end

  # SQL queries are off on this instance (InstanceSetting#sql_queries_enabled),
  # so Console refuses them before asking EventStore, and EventStore refuses
  # them too (ADR 0003).
  class SQLQueriesOff < StandardError
    def initialize(message = "SQL queries are turned off on this instance.")
      super
    end
  end

  # How much longer than a run's timeout_seconds Console waits for EventStore
  # to answer.
  TIMEOUT_GRACE_SECONDS = 5

  # A Log's id: a UUID.
  LOG_ID = /\A\h{8}-\h{4}-\h{4}-\h{4}-\h{12}\z/

  # The event store's query API, which listens on loopback only.
  def self.base_url
    ENV.fetch("EVENT_STORE_URL", "http://127.0.0.1:4100")
  end

  # The event store's ingest, where SDKs and shippers send, as Console
  # reaches it.
  def self.ingest_url
    ENV.fetch("EVENT_STORE_INGEST_URL", "http://127.0.0.1:4000")
  end

  # Ingest as SDKs and shippers reach it: PUBLIC_INGEST_URL, or else
  # Console's host, as console_url names it, at EventStore's ingest port. The
  # Logs snippets, the Project page's DSN and the MCP server send there.
  def self.public_ingest_url(console_url)
    return ENV["PUBLIC_INGEST_URL"].chomp("/") if ENV["PUBLIC_INGEST_URL"].present?

    console = URI(console_url)
    "#{console.scheme}://#{console.host}:4000"
  end

  # ============================================================================
  # Class Methods - Direct API Access
  # ============================================================================

  class << self
    def get_event(project_id:, event_uuid:)
      url = "#{base_url}/api/#{project_id}/events/#{event_uuid}"
      make_request(url)
    end

    # Raises Unavailable when EventStore cannot answer, such as its 503
    # while it can't read Events.
    def query_events!(project_id:, params: {})
      request!("#{base_url}/api/#{project_id}/events", params: params, not_found: [])
    end

    # Raises Unavailable when EventStore cannot answer, as query_events! does.
    def query_event_with_context!(project_id:, params: {})
      request!("#{base_url}/api/#{project_id}/events/context", params: params)
    end

    def count_events(project_id:, params: {})
      count_events!(project_id: project_id, params: params)
    rescue Unavailable => e
      Rails.logger.error(e.message)
      0
    end

    # Like count_events, but raises Unavailable instead of counting 0 when
    # EventStore cannot answer.
    def count_events!(project_id:, params: {})
      url = "#{base_url}/api/#{project_id}/events/count"
      request!(url, params: params, not_found: { "count" => 0 })["count"]
    end

    # One page of a Project's Logs that match a Query in [from, to), newest
    # first: {"logs" => [...], "next_cursor" => String | nil}, or a tabular
    # Query's {"table" => {...}}. Pass a page's next_cursor as cursor for the
    # page of older Logs. With totals, the table of a Query that ends in a
    # summarize by a time bin also has "totals": a row per group, the other
    # by columns, with its aggregates over the whole range. With
    # timeout_seconds, EventStore stops the run after that long and this
    # raises TimedOut; Console waits a little longer, so EventStore's answer
    # comes first.
    def query_logs(project_id:, query:, from:, to:, cursor: nil, limit: nil, totals: false, timeout_seconds: nil)
      body = { query: query, from: utc(from), to: utc(to) }
      body[:cursor] = cursor if cursor.present?
      body[:limit] = limit if limit
      body[:totals] = true if totals
      body[:timeout_seconds] = timeout_seconds if timeout_seconds
      post!("#{base_url}/api/#{project_id}/logs/query", body, read_timeout: read_timeout(timeout_seconds))
    end

    # A Match monitor's run: the Logs of a Project's Log batches after
    # after_batch, up to the batch Log processing has stored last, that match
    # a Query: {"logs" => [...], "total", "ingest_cursor"}, at most limit
    # Logs, newest first, and total, how many matched in all. ingest_cursor
    # is the after_batch of the next run. after_batch "" reads nothing and
    # answers the ingest_cursor to start from, which arms a Monitor; it is
    # never nil. A Query that returns a table is Rejected. Raises TimedOut
    # past timeout_seconds, when the run reads nothing and the next run starts
    # from the same after_batch.
    def logs_ingested_after(project_id:, query:, after_batch:, limit: nil, timeout_seconds: nil)
      body = { query: query, after_batch: after_batch.to_s }
      body[:limit] = limit if limit
      body[:timeout_seconds] = timeout_seconds if timeout_seconds
      post!("#{base_url}/api/#{project_id}/logs/query", body, read_timeout: read_timeout(timeout_seconds))
    end

    # What EventStore said in an error body it answered: the message of each
    # error, joined, or the body as text.
    def error_text(body)
      return body.to_s unless body.is_a?(Hash)

      messages = Array(body["errors"]).filter_map { |error| error.is_a?(Hash) ? error["message"] : error.to_s }
      messages << body["error"] if body["error"].present?
      messages.any? ? messages.join("; ") : body.to_json
    end

    # Why EventStore couldn't read Events, from the Unavailable it raised:
    # what it said, or that it can't be reached.
    def events_unavailable_text(error)
      error.body ? error_text(error.body) : "Events can't be read now: EventStore can't be reached."
    end

    # A Query's mistakes in a refusal or a validate answer, one line each,
    # with its line and column when it has them; or what else EventStore
    # said, as one line.
    def mistakes(body)
      errors = body.is_a?(Hash) ? Array(body["errors"]).select { |error| error.is_a?(Hash) } : []
      return [ error_text(body) ] if errors.empty?

      errors.map do |error|
        error["line"] ? "line #{error['line']}, column #{error['column']}: #{error['message']}" : error["message"].to_s
      end
    end

    # Counts the Logs in [from, to) that pass a Query's filters, per bucket
    # and Level: {"bucket_seconds", "buckets" => [{"time", "count",
    # "levels"}], "count", "levels"}. levels has every Level and "none" for
    # Logs with no Level. Without bucket_seconds EventStore picks the size.
    # timeout_seconds works as in query_logs.
    def logs_histogram(project_id:, query:, from:, to:, bucket_seconds: nil, timeout_seconds: nil)
      body = { query: query, from: utc(from), to: utc(to) }
      body[:bucket_seconds] = bucket_seconds if bucket_seconds
      body[:timeout_seconds] = timeout_seconds if timeout_seconds
      post!("#{base_url}/api/#{project_id}/logs/histogram", body, read_timeout: read_timeout(timeout_seconds))
    end

    # Checks a Query without running it: {"errors" => [...], "chips" => Hash | nil,
    # "stages" => [...]}, where stages places each stage of a Query without
    # mistakes and says whether the Builder can show it.
    def validate_logs_query(project_id:, query:)
      post!("#{base_url}/api/#{project_id}/logs/validate", { query: query })
    end

    # One of a Project's Logs, or nil. An id that isn't a Log id (LOG_ID) is
    # nil without asking EventStore. time, the Log's own time, narrows the
    # search to that hour.
    def get_log(project_id:, log_id:, time: nil)
      return unless LOG_ID.match?(log_id.to_s)

      params = time.present? ? { time: time } : {}
      request!("#{base_url}/api/#{project_id}/logs/#{log_id}", params: params)
    end

    # A Project's Fields over [from, to), for learning what a Query can read:
    # {"built_in" => [{"name", "type"}], "attributes" => [{"name", "type",
    # "logs"}], "truncated"}. built_in are the built-in fields; attributes the
    # top Attributes of the UTC days the range touches, most Logs first, at
    # most limit (EventStore's default 100, at most 500), and truncated
    # whether there were more. An Attribute's type is "string", "number" or
    # "bool", and a name whose values have two types is listed for each.
    def logs_fields(project_id:, from:, to:, limit: nil)
      params = { from: utc(from), to: utc(to) }
      params[:limit] = limit if limit
      get!("#{base_url}/api/#{project_id}/logs/fields", params)
    end

    # A Query printed as readable SQL against the logs view of a Project's
    # [from, to): {"sql" => String}. It runs nothing, but seeds SQL mode, so
    # it is refused like Run SQL while SQL queries are off.
    def readable_logs_sql(project_id:, query:, from:, to:)
      refuse_sql_unless_enabled!
      post!("#{base_url}/api/#{project_id}/logs/readable-sql", { query: query, from: utc(from), to: utc(to) }, sql: true)
    end

    # Runs SQL as written against the logs view of a Project's [from, to):
    # {"table" => {"columns", "rows", "truncated"}}, shaped like a tabular
    # Query's run. Raises SQLQueriesOff while SQL queries are off.
    # timeout_seconds works as in query_logs.
    def run_logs_sql(project_id:, sql:, from:, to:, timeout_seconds: nil)
      refuse_sql_unless_enabled!
      body = { sql: sql, from: utc(from), to: utc(to) }
      body[:timeout_seconds] = timeout_seconds if timeout_seconds
      post!("#{base_url}/api/#{project_id}/logs/sql", body, sql: true, read_timeout: read_timeout(timeout_seconds))
    end

    # Sends Logs to a Project's ingest as a shipper would, with one of its
    # keys: {"accepted", "dropped", "truncated"}. A 200 means they are stored;
    # Log processing makes them readable a moment later.
    def ingest_logs(project_id:, key:, logs:)
      post!("#{ingest_url}/api/#{project_id}/logs", logs, headers: { authorization: "Bearer #{key}" }, read_timeout: 10)
    end

    private

    def refuse_sql_unless_enabled!
      raise SQLQueriesOff unless InstanceSetting.current.sql_queries_enabled?
    end

    def utc(time)
      time.utc.iso8601(3)
    end

    # How long Console waits for a run given timeout_seconds: a little
    # longer, so EventStore's 504 comes first. nil, no limit, without one.
    def read_timeout(timeout_seconds)
      timeout_seconds && (timeout_seconds + TIMEOUT_GRACE_SECONDS)
    end

    # Pass read_timeout: nil for a run with no timeout_seconds, which waits as
    # long as its Query takes. Opening the connection still times out.
    # Pass sql: true for a SQL endpoint, whose 403 means SQL queries are off.
    def post!(url, body, sql: false, headers: {}, **timeouts)
      execute!(
        sql: sql, method: :post, url: url, payload: body.to_json,
        headers: { content_type: :json, accept: :json, **headers }, **timeouts
      )
    end

    # A Log call's GET, with params as its query string, answered as post!
    # answers.
    def get!(url, params)
      execute!(method: :get, url: url, headers: { accept: :json, params: params })
    end

    # Sends a Log call's request and parses EventStore's JSON answer. A 400
    # raises Rejected, a 504 TimedOut, and a 403 SQLQueriesOff when sql.
    def execute!(sql: false, **request)
      JSON.parse(RestClient::Request.execute(**request).body)
    rescue RestClient::BadRequest => e
      raise Rejected, parse_body(e.response&.body)
    rescue RestClient::Forbidden => e
      raise SQLQueriesOff if sql

      raise Unavailable, "EventStore error: 403 - #{e.response&.body}"
    rescue RestClient::GatewayTimeout => e
      raise TimedOut, parse_body(e.response&.body)
    rescue RestClient::ExceptionWithResponse => e
      raise Unavailable, "EventStore error: #{e.response&.code} - #{e.response&.body}"
    rescue StandardError => e
      raise Unavailable, "EventStore connection error: #{e.message}"
    end

    def parse_body(body)
      JSON.parse(body.to_s)
    rescue JSON::ParserError
      { "error" => body.to_s }
    end

    def make_request(url, params: {}, default: nil)
      request!(url, params: params, not_found: default)
    rescue Unavailable => e
      Rails.logger.error(e.message)
      default
    end

    def request!(url, params: {}, not_found: nil)
      response = RestClient.get(url, params: params)
      JSON.parse(response.body)
    rescue RestClient::NotFound
      not_found
    rescue RestClient::ExceptionWithResponse => e
      body = e.response&.body
      raise Unavailable.new("EventStore error: #{e.response&.code} - #{body}", body: body.presence && parse_body(body))
    rescue StandardError => e
      raise Unavailable, "EventStore connection error: #{e.message}"
    end
  end

  # ============================================================================
  # Instance Methods - Query Builder
  # ============================================================================

  def initialize(issue)
    @issue = issue
    @fingerprint_ids = issue.issue_fingerprint_ids
    @project_id = issue.project_id
    @limit = 20
    @offset = 0
    @order_dir = "DESC"
    @timestamp_filter = nil
    @timestamp_op = nil
    @uuid_filter = nil
  end

  # Chainable methods

  def where_uuid(uuid)
    @uuid_filter = uuid
    self
  end

  def order(direction)
    @order_dir = direction == :asc ? "ASC" : "DESC"
    self
  end

  def limit(limit)
    @limit = limit
    self
  end

  def offset(offset)
    @offset = offset
    self
  end

  def newer_than(timestamp)
    @timestamp_filter = timestamp
    @timestamp_op = ">"
    self
  end

  def older_than(timestamp)
    @timestamp_filter = timestamp
    @timestamp_op = "<"
    self
  end

  def all
    all!
  rescue Unavailable => e
    Rails.logger.error(e.message)
    []
  end

  # Like all, but raises Unavailable instead of answering [] when EventStore
  # cannot answer, so a page can say Events can't be read rather than show
  # none.
  def all!
    return [] if @fingerprint_ids.empty? && @uuid_filter.nil?

    project_id = resolve_project_id
    return [] unless project_id

    params = {
      limit: @limit,
      offset: @offset,
      sort: @order_dir == "DESC" ? "desc" : "asc",
    }

    if @uuid_filter
      params[:uuid] = @uuid_filter
    else
      params[:fingerprint_ids] = @fingerprint_ids.join(",")
    end

    params.merge!(timestamp_params)

    events_data = self.class.query_events!(project_id: project_id, params: params)
    events_data.map { |attrs| build_event(attrs) }
  end

  def count
    count!
  rescue Unavailable => e
    Rails.logger.error(e.message)
    0
  end

  # Like count, but raises Unavailable instead of counting 0 when EventStore
  # cannot answer.
  def count!
    return 0 if @fingerprint_ids.empty?

    project_id = resolve_project_id
    return 0 unless project_id

    params = { fingerprint_ids: @fingerprint_ids.join(",") }.merge(timestamp_params)
    self.class.count_events!(project_id: project_id, params: params)
  end

  def first
    limit(1).all.first
  end

  # Returns { event:, prev_uuid:, next_uuid: } in a single API call
  # If uuid_filter is set, fetches that event; otherwise fetches the latest event
  def get_event_with_context
    get_event_with_context!
  rescue Unavailable => e
    Rails.logger.error(e.message)
    nil
  end

  # Like get_event_with_context, but raises Unavailable instead of answering
  # nil when EventStore cannot answer.
  def get_event_with_context!
    return nil if @fingerprint_ids.empty? && @uuid_filter.nil?

    project_id = resolve_project_id
    return nil unless project_id

    params = { fingerprint_ids: @fingerprint_ids.join(",") }
    params[:uuid] = @uuid_filter if @uuid_filter

    response = self.class.query_event_with_context!(project_id: project_id, params: params)
    return nil unless response

    {
      event: build_event(response["event"]),
      prev_uuid: response["prev_uuid"].presence,
      next_uuid: response["next_uuid"].presence,
    }
  end

  private

  def timestamp_params
    return {} unless @timestamp_filter

    case @timestamp_op
    when ">" then { newer_than: @timestamp_filter.iso8601 }
    when "<" then { older_than: @timestamp_filter.iso8601 }
    else {}
    end
  end

  def resolve_project_id
    @project_id
  end

  def build_event(attrs)
    Event.new(
      uuid: attrs["uuid"],
      project_id: attrs["project_id"],
      issue_fingerprint_id: attrs["issue_fingerprint_id"],
      environment: attrs["environment"],
      timestamp: Time.parse(attrs["timestamp"]),
      tags: attrs["tags"] || {}
    )
  end
end
