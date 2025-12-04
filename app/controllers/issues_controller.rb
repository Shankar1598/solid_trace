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
  end

  def show
    @issue = scoped_resources.find(params[:id])
    @latest_event = @issue.issue_events.order(created_at: :desc).first
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
