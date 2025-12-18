# frozen_string_literal: true

class IssueSerializer
  def initialize(issue)
    @issue = issue
  end

  def as_json(*)
    {
      id: @issue.id,
      number: @issue.number,
      title: @issue.title,
      culprit: @issue.culprit,
      status: @issue.status,
      kind: @issue.kind,
      events_count: @issue.events.count,
      created_at: @issue.created_at.iso8601,
      updated_at: @issue.updated_at.iso8601,
      project: {
        id: @issue.project.id,
        name: @issue.project.name,
        slug: @issue.project.slug,
      },
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
