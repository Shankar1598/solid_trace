# frozen_string_literal: true

# A Project's Performance page, its Transactions by name, and a Transaction's
# page (?name=), over the range and environment in the URL: range=<span> or
# from and to, as on Logs, and environment, every one without it. Any member
# sees them; TransactionsView and TransactionView build them, as they do for
# list_transactions and get_transaction. The Organization's Performance
# opens the Project last seen there, as Logs does.
class PerformanceController < ApplicationController
  layout "dashboard"

  before_action :set_current_org
  before_action :set_project, except: :start

  def start
    project = @current_org.projects.find_by(id: last_performance_project_ids[@current_org.id.to_s]) ||
      @current_org.projects.order(created_at: :desc).first
    if project
      redirect_to project_performance_path(project, org_slug: @current_org.slug)
    else
      redirect_to projects_path(org_slug: @current_org.slug), notice: "Create a Project to see its Performance."
    end
  end

  def show
    session[:last_performance_project_ids] = last_performance_project_ids.merge(@current_org.id.to_s => @project.id)
    view = TransactionsView.new(project: @project, range: range, environment: params[:environment])
    render inertia: "Performance/Show", props: { performance: view.to_h }
  end

  def transaction
    view = TransactionView.new(project: @project, name: params[:name], range: range,
      environment: params[:environment])
    render inertia: "Performance/Transaction", props: { transaction: view.to_h }
  end

  private

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end

  def last_performance_project_ids
    session[:last_performance_project_ids] || {}
  end

  # The URL's range, or the default one when it names none that can be read.
  def range
    TransactionsView.range(range: params[:range], from: params[:from], to: params[:to])
  rescue McpTools::ToolError
    TransactionsView.range
  end
end
