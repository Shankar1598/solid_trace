# frozen_string_literal: true

# A page of an Organization's Issues, filtered and sorted as the Issues page
# lists them. The MCP server's list_issues reads the same pages.
class IssueList
  PER_PAGE = 25

  # An Issue with no counted Event yet is seen when it was created.
  LAST_SEEN = "COALESCE(issues.last_seen_at, issues.created_at)"
  SORTS = {
    "last_seen" => Arel.sql("#{LAST_SEEN} DESC, issues.id DESC"),
    "first_seen" => Arel.sql("COALESCE(issues.first_seen_at, issues.created_at) DESC, issues.id DESC"),
  }.freeze

  # How long ago an Issue may have last been seen and still be listed.
  RANGES = {
    "24h" => 24.hours,
    "14d" => 14.days,
    "30d" => 30.days,
  }.freeze

  attr_reader :sort, :page, :total_count, :total_pages

  # status is an Issue status, or "all" or blank for every status. query is
  # text the title holds, and project_id a Project's id, or "all" or blank
  # for every Project. range is a key of RANGES, or blank for Issues seen at
  # any time. sort is a key of SORTS, last_seen otherwise, and page counts
  # from 1, kept within the pages there are.
  def initialize(organization, status: nil, query: nil, project_id: nil, range: nil, sort: nil, page: nil)
    @issues = Issue.where(project_id: organization.projects.select(:id))
    @issues = @issues.where(status: status) if status.present? && status != "all"
    @issues = @issues.where("title LIKE ?", "%#{query}%") if query.present?
    @issues = @issues.where(project_id: project_id) if project_id.present? && project_id != "all"
    @issues = @issues.where("#{LAST_SEEN} >= ?", RANGES.fetch(range).ago) if RANGES.key?(range)

    @sort = SORTS.key?(sort) ? sort : "last_seen"
    @total_count = @issues.count
    @total_pages = [ (total_count.to_f / PER_PAGE).ceil, 1 ].max
    @page = page.to_i.clamp(1, total_pages)
  end

  # The page's Issues, with their Projects and assignees.
  def issues
    @issues.order(SORTS[sort]).offset((page - 1) * PER_PAGE).limit(PER_PAGE).includes(:project, assignee: :user)
  end
end
