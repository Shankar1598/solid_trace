# frozen_string_literal: true

module McpTools
  # What the Issue tools share: the issue argument, the Issue it names in its
  # Project, and an Issue and its comments as the tools answer them. Any
  # member of the Organization can read, resolve, assign and comment on its
  # Issues, as in the UI.
  module IssueTool
    extend ActiveSupport::Concern

    ISSUE = { type: "integer", description: "The Issue's number in its Project. list_issues lists them." }.freeze

    class_methods do
      # The organization, project and issue arguments, which #issue reads.
      def takes_issue
        takes_project
        argument :issue, ISSUE, required: true
      end
    end

    private

    # The Issue the issue argument numbers, in #project.
    def issue
      @issue ||= project.issues.includes(assignee: :user).find_by!(number: arguments[:issue])
    end

    # An Issue as the Issues page lists it, with the slug of its Project and
    # its number, which the tools take, and its assignee's name and email.
    def issue_json(issue)
      { project: issue.project.slug }
        .merge(IssueSerializer.new(issue).as_json.slice(:number, :title, :culprit, :status, :kind, :events_count, :first_seen_at, :last_seen_at))
        .merge(assignee: issue.assignee && person_json(issue.assignee.user))
    end

    # A comment as plain text, with who wrote it.
    def comment_json(comment)
      { id: comment.id, user: person_json(comment.user), body: comment.content.to_plain_text, created_at: comment.created_at.iso8601 }
    end

    def person_json(user)
      { name: user.name, email: user.email }
    end
  end
end
