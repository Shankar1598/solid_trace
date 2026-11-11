# frozen_string_literal: true

# The Organization's settings page: its name and slug, and Require
# two-factor, which only an admin who has Two-factor turns on or off. No MCP
# tool reads or changes Require two-factor, so a leaked API token can't.
class OrganizationSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org

  def show
    render inertia: "Settings/Organization", props: {
      organization: OrganizationSerializer.new(@current_org, include_members: true).as_json,
      tab: params[:tab] || "general",
      require_two_factor: {
        on: @current_org.require_two_factor,
        can_change: @current_org.can_require_two_factor?(current_user),
        you_have_two_factor: current_user.two_factor?,
        members_without_two_factor: @current_org.members_without_two_factor,
      },
    }
  end

  def update
    if organization_params.key?(:require_two_factor) && !@current_org.can_require_two_factor?(current_user)
      redirect_to organization_path(org_slug: @current_org.slug),
        alert: "Only an admin who has Two-factor can change Require two-factor."
    elsif @current_org.update(organization_params)
      redirect_to organization_path(org_slug: @current_org.slug), notice: "Organization settings updated"
    else
      redirect_to organization_path(org_slug: @current_org.slug), inertia: { errors: @current_org.errors.to_hash }
    end
  end

  private

  def organization_params
    params.require(:organization).permit(:name, :slug, :require_two_factor)
  end
end
