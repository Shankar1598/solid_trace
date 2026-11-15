# frozen_string_literal: true

# Adds and removes the tracker routes of a Project's Factory settings at the
# Dispatcher (FactorySettings). Only an Admin of the Organization changes
# them; anyone else gets 403, with no call to the Dispatcher. With no
# Dispatcher URL, 404.
class FactoryRoutesController < ApplicationController
  before_action :set_current_org
  before_action :set_project

  # The Dispatcher's statuses for a route that it refuses (a bad value, a
  # pair in the list two times, no such tracker connection, a pair that
  # another Project has): the section shows them under the routes.
  FORM_ERRORS = [ 400, 409, 422 ].freeze

  def create
    change("Route added") { |factory| factory.add_route(route[:connection], route[:match]) }
  end

  def destroy
    change("Route removed") { |factory| factory.remove_route(route[:connection], route[:match]) }
  end

  private

  def set_project
    raise ActiveRecord::RecordNotFound unless Dispatcher.on?

    @project = @current_org.projects.find_by!(slug: params[:project_slug])
    head :forbidden unless policy(@project).update?
  end

  def route
    params.fetch(:route, {}).permit(:connection, :match)
  end

  def change(notice)
    back = project_path(@project, org_slug: @current_org.slug)
    yield FactorySettings.new(user: current_user, project: @project)
    redirect_to back, notice: notice
  rescue FactorySettings::NoSettings => e
    redirect_to back, inertia: { errors: { factory_routes: e.message } }
  rescue Dispatcher::Refused, Dispatcher::Unavailable => e
    if e.is_a?(Dispatcher::Refused) && FORM_ERRORS.include?(e.status)
      redirect_to back, inertia: { errors: { factory_routes: FactorySettings.message(e) } }
    else
      redirect_to back, alert: "The Dispatcher did not save the routes: #{e.message}"
    end
  end
end
