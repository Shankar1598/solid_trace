# frozen_string_literal: true

module McpTools
  # Lists an Organization's Issues, a page at a time, filtered and sorted as
  # its Issues page does (IssueList).
  class ListIssues < Base
    include IssueTool

    description "Lists the Organization's Issues, #{IssueList::PER_PAGE} a page, the most recently seen first, as its Issues page does. " \
                "Each has its Project and number, which get_issue takes, its title, culprit, status, kind, Event count, " \
                "first and last seen, and assignee. Narrow the list by project, status and text in the title."
    read_only!
    takes_organization
    argument :project, { type: "string", description: "Only this Project's Issues, by slug. Every Project's when left out. list_projects lists them." }
    argument :status, { type: "string", enum: [ *Issue.statuses.keys, "all" ], description: "Only Issues with this status. all when left out." }
    argument :query, { type: "string", description: "Only Issues whose title holds this text." }
    argument :sort, {
      type: "string",
      enum: IssueList::SORTS.keys,
      description: "last_seen, the default, lists the Issues seen most recently first; first_seen lists the newest Issues first.",
    }
    argument :page, { type: "integer", minimum: 1, description: "The page to give, from 1. 1 when left out." }

    def run
      filters = { status: arguments[:status], query: arguments[:query].presence, sort: arguments[:sort] }
      filters[:project_id] = project.id if arguments.key?(:project)
      list = IssueList.new(organization, page: arguments[:page], **filters)

      json({
        organization: organization.slug,
        issues: list.issues.map { |issue| issue_json(issue) },
        page: list.page,
        total_pages: list.total_pages,
        total_count: list.total_count,
      }, link: links.issues(organization, **filters, page: (list.page if list.page > 1)))
    end
  end
end
