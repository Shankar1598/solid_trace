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
    if params[:environment].present? && params[:environment] != "all"
      @issues = @issues.joins(:events).where(events: { environment: params[:environment] }).distinct
    end

    @environments = Event.where(issue_id: scoped_resources.select(:id)).distinct.pluck(:environment).compact.sort

    render inertia: "Issues/Index", props: {
      issues: @issues.includes(:project).map { |i| IssueSerializer.new(i).as_json },
      environments: @environments,
      filters: {
        status: params[:status] || "all",
        query: params[:query] || "",
        environment: params[:environment] || "all"
      }
    }
  end

  def show
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    @issue = @project.issues.find_by!(number: params[:number])
    events = @issue.events.order(created_at: :desc)

    if params[:environment].present? && params[:environment] != "all"
      events = events.where(environment: params[:environment])
    end

    if params[:event_id].present?
      @event = events.find_by(id: params[:event_id])
    end

    # Fallback to latest if not found
    @event ||= events.first

    if @event
      # Newer event (Next) - need to reorder to ASC to get the closest newer event
      @next_event = events.where("created_at > ?", @event.created_at).reorder(created_at: :asc).first
      # Older event (Previous) - need to reorder to DESC to get the closest older event
      @prev_event = events.where("created_at < ?", @event.created_at).reorder(created_at: :desc).first
    end

    @environments = Event.where(issue_id: scoped_resources.select(:id)).distinct.pluck(:environment).compact.sort

    render inertia: "Issues/Show", props: {
      issue: IssueSerializer.new(@issue).as_json,
      event: @event ? EventSerializer.new(@event).as_json : nil,
      prev_event_id: @prev_event&.id,
      next_event_id: @next_event&.id,
      environments: @environments,
      comments: @issue.comments.includes(:user).order(created_at: :asc).map { |c| CommentSerializer.new(c).as_json },
      current_environment: params[:environment] || "all"
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
