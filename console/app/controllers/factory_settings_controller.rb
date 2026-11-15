# frozen_string_literal: true

# Saves the Factory section of a Project's settings at the Dispatcher
# (FactorySettings). Only an Admin of the Organization saves; anyone else
# gets 403, with no call to the Dispatcher. With no Dispatcher URL, 404.
class FactorySettingsController < ApplicationController
  before_action :set_current_org

  # The Dispatcher's statuses for a value that it refuses: the form shows
  # them under its fields.
  FORM_ERRORS = [ 400, 422 ].freeze

  def update
    raise ActiveRecord::RecordNotFound unless Dispatcher.on?

    project = @current_org.projects.find_by!(slug: params[:project_slug])
    return head :forbidden unless policy(project).update?

    back = project_path(project, org_slug: @current_org.slug)
    FactorySettings.new(user: current_user, project: project)
      .save(params.fetch(:factory, {}).permit(*FactorySettings::FIELDS).to_h)
    redirect_to back, notice: "Factory settings saved"
  rescue Dispatcher::Refused, Dispatcher::Unavailable => e
    if e.is_a?(Dispatcher::Refused) && FORM_ERRORS.include?(e.status)
      redirect_to back, inertia: { errors: { factory: FactorySettings.message(e) } }
    else
      redirect_to back, alert: "The Dispatcher did not save the Factory settings: #{e.message}"
    end
  end
end
