# frozen_string_literal: true

class Event
  attr_reader :uuid, :project_id, :issue_fingerprint_id, :timestamp, :tags

  def initialize(attrs)
    @uuid = attrs[:uuid]
    @project_id = attrs[:project_id]
    @issue_fingerprint_id = attrs[:issue_fingerprint_id]
    @timestamp = attrs[:timestamp]
    @tags = attrs[:tags] || {}
    @payload = attrs[:payload]  # Pre-loaded if available
  end

  # Lazy-load payload from Go service
  def payload
    @payload ||= EventStore.get_event(
      project_id: project_id,
      event_uuid: uuid,
      timestamp: timestamp
    )
  end

  # Compatibility methods
  def id
    uuid
  end

  def created_at
    timestamp
  end

  def issue_fingerprint
    @issue_fingerprint ||= IssueFingerprint.find(issue_fingerprint_id)
  end

  def issue
    issue_fingerprint&.issue
  end

  def project
    @project ||= Project.find(project_id)
  end

  def environment
    tags["environment"] || "unknown"
  end
end
