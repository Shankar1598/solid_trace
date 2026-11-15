# frozen_string_literal: true

# Pauses and continues a Project's schedule at the Dispatcher, at once.
# Each member of the Organization can do this, as with the CLI: a member
# who sees a schedule misbehave stops it with no Admin. With no Dispatcher
# URL, 404. Console has no form to make a schedule.
class FactorySchedulesController < ApplicationController
  before_action :set_current_org
  before_action :set_project

  def pause
    change("pause", "paused")
  end

  def continue
    change("continue", "continues")
  end

  private

  def set_project
    raise ActiveRecord::RecordNotFound unless Dispatcher.on?

    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end

  def change(action, done)
    back = project_path(@project, org_slug: @current_org.slug)
    schedule = FactorySettings.new(user: current_user, project: @project).change_schedule(params[:id], action)
    name = schedule.is_a?(Hash) ? schedule["name"].presence : nil
    redirect_to back, notice: "Schedule #{name || params[:id]} #{done}"
  rescue Dispatcher::Refused, Dispatcher::Unavailable => e
    redirect_to back, alert: "The Dispatcher did not #{action} the schedule: #{e.message}"
  end
end
