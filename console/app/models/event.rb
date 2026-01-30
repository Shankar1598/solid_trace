# frozen_string_literal: true

class Event
  attr_reader :uuid, :project_id, :issue_fingerprint_id, :timestamp, :tags, :environment

  def initialize(attrs)
    @uuid = attrs[:uuid]
    @project_id = attrs[:project_id]
    @issue_fingerprint_id = attrs[:issue_fingerprint_id]
    @timestamp = attrs[:timestamp]
    @environment = attrs[:environment]
    @tags = attrs[:tags] || {}
    @payload = attrs[:payload]  # Pre-loaded if available
  end

  def as_json
    {
      uuid: uuid,
      project_id: project_id,
      issue_fingerprint_id: issue_fingerprint_id,
      timestamp: timestamp,
      tags: tags,
      environment: environment,
    }
  end

  # Lazy-load payload from Go service
  def payload
    @payload ||= EventStore.get_event(uuid)
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
end
