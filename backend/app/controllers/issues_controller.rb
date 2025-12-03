class IssuesController < ApplicationController
  before_action :set_organization

  def index
    @issues = @organization.issues.includes(:project).order(updated_at: :desc)
  end

  def show
    @issue = @organization.issues.find(params[:id])
    @events = @issue.issue_events.order(created_at: :desc).limit(50)
  end

  private

  def set_organization
    @organization = Organization.find_by!(slug: params[:org_slug])
  end
end
