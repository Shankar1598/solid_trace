# frozen_string_literal: true

require "rest-client"
require "json"

class EventStore
  def self.base_url
    ENV.fetch("EVENT_STORE_URL", "http://localhost:4000")
  end

  # ============================================================================
  # Class Methods - Direct API Access
  # ============================================================================

  class << self
    def get_event(event_uuid)
      url = "#{base_url}/api/events/#{event_uuid}"
      make_request(url)
    end

    def query_events(project_id:, params: {})
      url = "#{base_url}/api/#{project_id}/events"
      make_request(url, params: params, default: [])
    end

    def count_events(project_id:, params: {})
      url = "#{base_url}/api/#{project_id}/events/count"
      response = make_request(url, params: params, default: { "count" => 0 })
      response["count"]
    end

    private

    def make_request(url, params: {}, default: nil)
      begin
        response = RestClient.get(url, params: params)
        JSON.parse(response.body)
      rescue RestClient::NotFound
        default
      rescue RestClient::ExceptionWithResponse => e
        Rails.logger.error("EventStore error: #{e.response.code} - #{e.response.body}")
        default
      rescue StandardError => e
        Rails.logger.error("EventStore connection error: #{e.message}")
        default
      end
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

    if @timestamp_filter
      if @timestamp_op == ">"
        params[:newer_than] = @timestamp_filter.iso8601
      elsif @timestamp_op == "<"
        params[:older_than] = @timestamp_filter.iso8601
      end
    end

    events_data = self.class.query_events(project_id: project_id, params: params)
    events_data.map { |attrs| build_event(attrs) }
  end

  def count
    return 0 if @fingerprint_ids.empty?

    project_id = resolve_project_id
    return 0 unless project_id

    params = { fingerprint_ids: @fingerprint_ids.join(",") }
    self.class.count_events(project_id: project_id, params: params)
  end

  def first
    limit(1).all.first
  end

  private

  def resolve_project_id
    @project_id
  end

  def build_event(attrs)
    Event.new(
      uuid: attrs["uuid"],
      project_id: attrs["project_id"],
      issue_fingerprint_id: attrs["issue_fingerprint_id"],
      timestamp: Time.parse(attrs["timestamp"]),
      tags: attrs["tags"] || {}
    )
  end
end
