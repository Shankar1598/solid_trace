# frozen_string_literal: true

class IssueSerializer
  def initialize(issue)
    @issue = issue
  end

  def as_json(*)
    assignee = @issue.assignee

    {
      id: @issue.id,
      number: @issue.number,
      title: @issue.title,
      culprit: @issue.culprit,
      status: @issue.status,
      kind: @issue.kind,
      events_count: @issue.times_seen,
      created_at: @issue.created_at.iso8601,
      updated_at: @issue.updated_at.iso8601,
      # Until an Event is counted, the Issue is seen when it was created.
      first_seen_at: (@issue.first_seen_at || @issue.created_at).iso8601,
      last_seen_at: (@issue.last_seen_at || @issue.created_at).iso8601,
      assignee: assignee ? { id: assignee.id, discarded_at: assignee.discarded_at&.iso8601, user: UserSerializer.new(assignee.user).as_json } : nil,
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
