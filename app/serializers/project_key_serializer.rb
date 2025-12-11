# frozen_string_literal: true

class ProjectKeySerializer
  def initialize(project_key)
    @project_key = project_key
  end

  def as_json(*)
    {
      id: @project_key.id,
      public_key: @project_key.public_key,
      label: @project_key.label,
      dsn: @project_key.dsn,
      created_at: @project_key.created_at.iso8601
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
