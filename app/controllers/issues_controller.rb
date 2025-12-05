class IssuesController < ApplicationController
  layout "dashboard"
  before_action :set_organization

  def index
    @issues = scoped_resources.order(created_at: :desc)

    if params[:level].present? && params[:level] != 'all'
      @issues = @issues.where(level: params[:level])
    end

    if params[:status].present? && params[:status] != 'all'
      @issues = @issues.where(status: params[:status])
    end

    if params[:query].present?
      @issues = @issues.where("title ILIKE ?", "%#{params[:query]}%")
    end
    if params[:environment].present? && params[:environment] != 'all'
      @issues = @issues.joins(:events).where(events: { environment: params[:environment] }).distinct
    end

    @environments = Event.where(issue_id: scoped_resources.select(:id)).distinct.pluck(:environment).sort
  end

  def show
    @issue = scoped_resources.find(params[:id])
    events = @issue.events.order(created_at: :desc)

    if params[:environment].present? && params[:environment] != 'all'
      events = events.where(environment: params[:environment])
    end

    if params[:event_id].present?
      @event = events.find_by(id: params[:event_id])
    end

    # Fallback to latest if not found
    @event ||= events.last

    if @event
      # Newer event (Next) - need to reorder to ASC to get the closest newer event
      @next_event = events.where("created_at > ?", @event.created_at).reorder(created_at: :asc).first
      # Older event (Previous) - need to reorder to DESC to get the closest older event
      @prev_event = events.where("created_at < ?", @event.created_at).reorder(created_at: :desc).first
    end

    @environments = Event.where(issue_id: scoped_resources.select(:id)).distinct.pluck(:environment).sort
  end

  def resolve
    @issue = scoped_resources.find(params[:id])
    @issue.update!(status: 1) # resolved
    redirect_to issue_path(@issue, org_slug: @current_org.slug), notice: "Issue resolved"
  end

  def unresolve
    @issue = scoped_resources.find(params[:id])
    @issue.update!(status: 0) # unresolved
    redirect_to issue_path(@issue, org_slug: @current_org.slug), notice: "Issue unresolved"
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def scoped_resources
    Issue.joins(project: :organization).where(organizations: { id: @current_org.id })
  end
end
