# frozen_string_literal: true

require "rest-client"
require "json"

class EventStore
  # EventStore could not answer: an error response, a connection error, a
  # timeout or an unreadable body.
  class Unavailable < StandardError; end

  # The event store's query API, which listens on loopback only.
  def self.base_url
    ENV.fetch("EVENT_STORE_URL", "http://127.0.0.1:4100")
  end

  # ============================================================================
  # Class Methods - Direct API Access
  # ============================================================================

  class << self
    def get_event(project_id:, event_uuid:)
      url = "#{base_url}/api/#{project_id}/events/#{event_uuid}"
      make_request(url)
    end

    def query_events(project_id:, params: {})
      url = "#{base_url}/api/#{project_id}/events"
      make_request(url, params: params, default: [])
    end

    def query_event_with_context(project_id:, params: {})
      url = "#{base_url}/api/#{project_id}/events/context"
      make_request(url, params: params)
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

    private

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
      raise Unavailable, "EventStore error: #{e.response&.code} - #{e.response&.body}"
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

    events_data = self.class.query_events(project_id: project_id, params: params)
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
    return nil if @fingerprint_ids.empty? && @uuid_filter.nil?

    project_id = resolve_project_id
    return nil unless project_id

    params = { fingerprint_ids: @fingerprint_ids.join(",") }
    params[:uuid] = @uuid_filter if @uuid_filter

    response = self.class.query_event_with_context(project_id: project_id, params: params)
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
