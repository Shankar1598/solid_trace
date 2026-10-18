# frozen_string_literal: true

class IssuesController < ApplicationController
  layout "dashboard"
  before_action :set_organization

  def index
    list = IssueList.new(@current_org, status: params[:status], query: params[:query], project_id: params[:project_id],
      sort: params[:sort], page: params[:page])

    render inertia: "Issues/Index", props: {
      issues: list.issues.map { |i| IssueSerializer.new(i).as_json },
      pagination: {
        current_page: list.page,
        total_pages: list.total_pages,
        total_count: list.total_count,
      },
      projects: @current_org.projects.map { |p| ProjectSerializer.new(p).as_json },
      filters: {
        status: params[:status] || "all",
        query: params[:query] || "",
        project_id: params[:project_id] || "all",
        sort: list.sort,
      },
    }
  end

  def show
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.includes(assignee: :user).find_by!(number: params[:number])

    page = (params[:events_page] || 1).to_i
    per_page = 20
    load_events(page, per_page)
    @events_count = @issue.times_seen

    render inertia: "Issues/Show", props: {
      issue: IssueSerializer.new(@issue).as_json,
      event: @event ? EventSerializer.new(@event).as_json : nil,
      prev_event_id: @prev_event_id,
      next_event_id: @next_event_id,
      events_unavailable: @events_unavailable,
      events_list: @events_list.map { |e| EventListSerializer.new(e).as_json },
      events_pagination: {
        current_page: page,
        total_pages: (@events_count.to_f / per_page).ceil,
        total_count: @events_count,
      },
      comments: @issue.comments.includes(organization_user: :user).order(created_at: :asc).map { |c| CommentSerializer.new(c).as_json },
    }
  end

  def assign
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:number])

    assignee_id = params[:assignee_id]
    assignee = nil

    if assignee_id.present? && assignee_id != "unassigned"
      assignee = @current_org.organization_users.find(assignee_id)
    end

    @issue.update!(assignee: assignee)

    redirect_to project_issue_path(org_slug: @current_org.slug, project_slug: @project.slug, number: @issue.number), notice: "Assignee updated"
  end

  def resolve
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:number])
    @issue.update!(status: Issue::STATUS::RESOLVED)
    redirect_to project_issue_path(@project, @issue, org_slug: @current_org.slug), notice: "Issue resolved"
  end

  def unresolve
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:number])
    @issue.update!(status: Issue::STATUS::UNRESOLVED)
    redirect_to project_issue_path(@project, @issue, org_slug: @current_org.slug), notice: "Issue unresolved"
  end

  private

  # The Issue's Event (params[:event_id], else its latest) with the Events
  # either side, and a page of its Events, newest first. When EventStore
  # can't read Events, @events_unavailable says why, so the page doesn't show
  # the Issue as having none.
  def load_events(page, per_page)
    if (context = @issue.event_with_context!(params[:event_id]))
      @event = context[:event]
      @prev_event_id = context[:prev_uuid]
      @next_event_id = context[:next_uuid]
    end
    @events_list = @issue.events.order(:desc).offset((page - 1) * per_page).limit(per_page).all!
  rescue EventStore::Unavailable => e
    Rails.logger.error(e.message)
    @events_list = []
    @events_unavailable = EventStore.events_unavailable_text(e)
  end

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end
end
