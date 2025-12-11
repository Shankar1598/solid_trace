# frozen_string_literal: true

class ProjectSerializer
  def initialize(project, include_keys: false)
    @project = project
    @include_keys = include_keys
  end

  def as_json(*)
    result = {
      id: @project.id,
      name: @project.name,
      slug: @project.slug,
      platform: @project.platform,
      created_at: @project.created_at.iso8601,
      issues_count: @project.issues.count
    }

    if @include_keys
      result[:keys] = @project.project_keys.map do |key|
        ProjectKeySerializer.new(key).as_json
      end
    end

    result
  end

  def to_json(*)
    as_json.to_json
  end
end
