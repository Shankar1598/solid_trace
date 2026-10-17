# frozen_string_literal: true

module McpTools
  # Resolves, unresolves and assigns an Issue, as its page does.
  class UpdateIssue < Base
    include IssueTool

    UNASSIGNED = "unassigned"

    description "Resolves or unresolves an Issue, or assigns it to a member of its Organization, as its page does. " \
                "A new Event in a resolved Issue makes it unresolved again. Assigning tells the Organization's Integrations."
    writes!(open_world: true)
    takes_issue
    argument :status, { type: "string", enum: Issue.statuses.keys, description: "resolved or unresolved." }
    argument :assignee, {
      type: "string",
      description: "The email of the member of the Organization to assign the Issue to, or #{UNASSIGNED}. list_members lists them.",
    }

    def run
      raise ToolError, "Give status, assignee or both to change." unless arguments.key?(:status) || arguments.key?(:assignee)

      changes = arguments.slice(:status)
      changes[:assignee] = assignee if arguments.key?(:assignee)
      issue.update!(changes)

      json(issue_json(issue), link: links.issue(issue))
    end

    private

    # The member the assignee argument names by email, ignoring case, or
    # nil for unassigned.
    def assignee
      email = arguments[:assignee].to_s.strip
      return if email == UNASSIGNED
      raise ToolError, "Give assignee as a member's email, or #{UNASSIGNED}." if email.empty?

      organization.organization_users.includes(:user).joins(:user).find_by("LOWER(users.email) = ?", email.downcase) ||
        raise(ToolError, "No member #{email} in #{organization.slug}. list_members lists them.")
    end
  end
end
