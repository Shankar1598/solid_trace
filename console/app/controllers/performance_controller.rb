# frozen_string_literal: true

# A Project's Performance page, its Transactions by name, and a Transaction's
# page (?name=), over the range and environment in the URL: range=<span> or
# from and to, as on Logs, and environment, every one without it. Any member
# sees them; TransactionsView and TransactionView build them, as they do for
# list_transactions and get_transaction.
class PerformanceController < ApplicationController
  layout "dashboard"

  before_action :set_current_org
  before_action :set_project

  def show
    view = TransactionsView.new(project: @project, user: current_user, range: range, environment: params[:environment])
    render inertia: "Performance/Show", props: { performance: view.to_h }
  end

  def transaction
    view = TransactionView.new(project: @project, user: current_user, name: params[:name], range: range,
      environment: params[:environment])
    render inertia: "Performance/Transaction", props: { transaction: view.to_h }
  end

  private

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end

  # The URL's range, or the default one when it names none that can be read.
  def range
    TransactionsView.range(range: params[:range], from: params[:from], to: params[:to])
  rescue McpTools::ToolError
    TransactionsView.range
  end
end
