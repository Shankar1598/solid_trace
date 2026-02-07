# frozen_string_literal: true

class IssuesController < ApplicationController
  layout "dashboard"
  before_action :set_organization

  def index
    @issues = scoped_resources.order(created_at: :desc)

    if params[:status].present? && params[:status] != "all"
      @issues = @issues.where(status: params[:status])
    end

    if params[:query].present?
      @issues = @issues.where("title LIKE ?", "%#{params[:query]}%")
    end
    if params[:project_id].present? && params[:project_id] != "all"
      @issues = @issues.where(project_id: params[:project_id])
    end
    render inertia: "Issues/Index", props: {
      issues: @issues.includes(:project, assignee: :user).map { |i| IssueSerializer.new(i).as_json },
      projects: @current_org.projects.map { |p| ProjectSerializer.new(p).as_json },
      filters: {
        status: params[:status] || "all",
        query: params[:query] || "",
        project_id: params[:project_id] || "all",
      },
    }
  end

  def show
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.includes(assignee: :user).find_by!(number: params[:number])

    query = @issue.events.order(:desc)

    # Single API call to get event with prev/next context
    event_query = @issue.events
    event_query = event_query.where_uuid(params[:event_id]) if params[:event_id].present?
    event_context = event_query.get_event_with_context

    if event_context
      @event = event_context[:event]
      @prev_event_id = event_context[:prev_uuid]
      @next_event_id = event_context[:next_uuid]
    end

    page = (params[:events_page] || 1).to_i
    per_page = 20
    @events_list = query.offset((page - 1) * per_page).limit(per_page).all
    @events_count = query.count

    render inertia: "Issues/Show", props: {
      issue: IssueSerializer.new(@issue).as_json,
      event: @event ? EventSerializer.new(@event).as_json : nil,
      prev_event_id: @prev_event_id,
      next_event_id: @next_event_id,
      events_list: @events_list.map { |e| EventListSerializer.new(e).as_json },
      events_pagination: {
        current_page: page,
        total_pages: (@events_count.to_f / per_page).ceil,
        total_count: @events_count,
      },
      comments: @issue.comments.includes(organization_user: :user).order(created_at: :asc).map { |c| CommentSerializer.new(c).as_json },
      assignees: @current_org.organization_users.includes(:user).map { |ou| { id: ou.id, user: UserSerializer.new(ou.user).as_json } },
    }
  end

  def assign
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:number])

    assignee_id = params[:assignee_id].presence
    assignee = assignee_id ? @current_org.organization_users.find(assignee_id) : nil

    @issue.update!(assignee: assignee)

    redirect_to project_issue_path(@project, @issue, org_slug: @current_org.slug), notice: "Assignee updated"
  end

  def resolve
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:number])
    @issue.update!(status: 1) # resolved
    redirect_to project_issue_path(@project, @issue, org_slug: @current_org.slug), notice: "Issue resolved"
  end

  def unresolve
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:number])
    @issue.update!(status: 0) # unresolved
    redirect_to project_issue_path(@project, @issue, org_slug: @current_org.slug), notice: "Issue unresolved"
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def scoped_resources
    Issue.joins(project: :organization).where(organizations: { id: @current_org.id })
  end
end
