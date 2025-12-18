# frozen_string_literal: true

class IntegrationsController < ApplicationController
  layout "dashboard"
  before_action :set_organization
  before_action :set_integration, only: [ :edit, :update, :destroy ]

  def index
    @integrations = @current_org.integrations

    render inertia: "Integrations/Index", props: {
      integrations: @integrations.map { |i| IntegrationSerializer.new(i).as_json },
    }
  end

  def new
    render inertia: "Integrations/New", props: {
      providers: Integration::PROVIDERS,
    }
  end

  def create
    @integration = @current_org.integrations.new(integration_params)

    if @integration.save
      redirect_to integrations_path(org_slug: @current_org.slug), notice: "Integration created successfully."
    else
      redirect_to new_integration_path(org_slug: @current_org.slug), inertia: { errors: @integration.errors.to_hash }
    end
  end

  def edit
    render inertia: "Integrations/Edit", props: {
      integration: IntegrationSerializer.new(@integration).as_json,
      providers: Integration::PROVIDERS,
    }
  end

  def update
    if @integration.update(integration_params)
      redirect_to integrations_path(org_slug: @current_org.slug), notice: "Integration updated successfully."
    else
      redirect_to edit_integration_path(@integration, org_slug: @current_org.slug), inertia: { errors: @integration.errors.to_hash }
    end
  end

  def destroy
    @integration.destroy
    redirect_to integrations_path(org_slug: @current_org.slug), notice: "Integration deleted."
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def set_integration
    @integration = @current_org.integrations.find(params[:id])
  end

  def integration_params
    params.require(:integration).permit(
      :provider,
      :name,
      :active,
      settings: [
        :webhook_url,
        :routing_key,
        :severity,
        :recipients,
        :notify_on_new_issue,
        :notify_on_event_threshold,
        :event_threshold,
        :time_window_minutes
      ]
    )
  end
end
