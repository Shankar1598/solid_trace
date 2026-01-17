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
    render inertia: "Issues/Index", props: {
      issues: @issues.includes(:project).map { |i| IssueSerializer.new(i).as_json },
      filters: {
        status: params[:status] || "all",
        query: params[:query] || "",
      },
    }
  end

  def show
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:number])
    events = @issue.events.order(:desc)

    if params[:event_id].present?
      @event = events.where_uuid(params[:event_id]).first
    end

    # Fallback to latest if not found.
    # Note: accessing 'events' again creates a new query object naturally if we didn't mutate it?
    # Wait, EventQuery is mutable (returns self).
    # 'events' variable holds the query object.
    # If I did 'events.where_uuid(...)', I modified 'events' object!
    # I should be careful. 'Issue#events' returns a new instance.
    # So:
    #
    query = @issue.events.order(:desc)

    if params[:event_id].present?
       # We need a fresh query for lookup to not affect the list
       @event = @issue.events.where_uuid(params[:event_id]).first
    end

    @event ||= query.first

    if @event
       # Newer event
       @next_event = @issue.events.newer_than(@event.created_at).order(:asc).first
       # Older event
       @prev_event = @issue.events.older_than(@event.created_at).order(:desc).first
    end

    # Pagination for events list
    # logic: query is already ordered desc
    page = (params[:events_page] || 1).to_i
    per_page = 20
    @events_list = query.offset((page - 1) * per_page).limit(per_page).all
    @events_count = query.count

    render inertia: "Issues/Show", props: {
      issue: IssueSerializer.new(@issue).as_json,
      event: @event ? EventSerializer.new(@event).as_json : nil,
      prev_event_id: @prev_event&.uuid,
      next_event_id: @next_event&.uuid,
      events_list: @events_list.map { |e| EventListSerializer.new(e).as_json },
      events_pagination: {
        current_page: page,
        total_pages: (@events_count.to_f / per_page).ceil,
        total_count: @events_count,
      },
      comments: @issue.comments.includes(:user).order(created_at: :asc).map { |c| CommentSerializer.new(c).as_json },
    }
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
