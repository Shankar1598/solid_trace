# frozen_string_literal: true

class ProjectSerializer
  # include_keys adds the Project's keys, with DSNs for ingest at ingest_url.
  def initialize(project, include_keys: false, ingest_url: nil)
    @project = project
    @include_keys = include_keys
    @ingest_url = ingest_url
  end

  def as_json(*)
    result = {
      id: @project.id,
      name: @project.name,
      slug: @project.slug,
      platform: @project.platform,
      log_retention_days: @project.log_retention_days,
      created_at: @project.created_at.iso8601,
      issues_count: @project.issues.count,
    }

    if @include_keys
      result[:keys] = @project.project_keys.map do |key|
        ProjectKeySerializer.new(key, ingest_url: @ingest_url).as_json
      end
    end

    result
  end

  def to_json(*)
    as_json.to_json
  end
end
