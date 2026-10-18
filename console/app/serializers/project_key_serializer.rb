# frozen_string_literal: true

class ProjectKeySerializer
  # +ingest_url+: where SDKs reach ingest, for the DSN.
  def initialize(project_key, ingest_url:)
    @project_key = project_key
    @ingest_url = ingest_url
  end

  def as_json(*)
    {
      id: @project_key.id,
      public_key: @project_key.public_key,
      label: @project_key.label,
      dsn: @project_key.dsn(@ingest_url),
      created_at: @project_key.created_at.iso8601,
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
