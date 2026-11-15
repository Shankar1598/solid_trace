# frozen_string_literal: true

module McpTools
  # Adds a comment to an Issue as the user, as its page does.
  class AddComment < Base
    include IssueTool

    description "Adds a comment to an Issue, as you. The body is plain text; a blank line starts a new paragraph."
    writes!
    takes_issue
    argument :body, { type: "string", description: "The comment, as plain text." }, required: true

    def run
      body = arguments[:body].to_s.strip
      raise ToolError, "Give the comment's body." if body.empty?

      membership = organization.organization_users.find_by!(user: user)
      comment = issue.comments.create!(organization_user: membership, content: Comment.html_of(body))

      json({ project: project.slug, issue: issue.number, comment: comment_json(comment) }, link: links.issue(issue))
    end
  end
end
