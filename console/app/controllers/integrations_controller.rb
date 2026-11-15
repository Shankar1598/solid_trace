# frozen_string_literal: true

# Any member manages the Organization's Integrations, except a 'Factory'
# Integration: only an Admin makes, changes or deletes it, because it
# starts Runs and Console makes and deletes its Webhook sender at the
# Dispatcher as that Admin (FactorySender). A member gets 403, with no call
# to the Dispatcher.
class IntegrationsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org
  before_action :set_integration, only: [ :edit, :update, :destroy ]
  before_action :admin_for_factory, only: [ :update, :destroy ]

  def index
    @integrations = @current_org.integrations

    render inertia: "Integrations/Index", props: {
      integrations: @integrations.map { |i| IntegrationSerializer.new(i).as_json },
      can_manage_factory: admin?,
    }
  end

  def new
    render inertia: "Integrations/New", props: {
      providers: providers_to_make.index_with(&:titleize),
      defaults: Integration.new.rule_settings,
    }
  end

  def create
    @integration = @current_org.integrations.new(integration_params.merge(params.require(:integration).permit(:provider)))
    return head :forbidden if @integration.factory? && !admin?
    return refuse_new(@integration.errors.to_hash) unless @integration.valid?
    return make_factory_integration if @integration.factory?

    @integration.save!
    redirect_to integrations_path(org_slug: @current_org.slug), notice: "Integration created successfully."
  end

  def edit
    render inertia: "Integrations/Edit", props: {
      integration: IntegrationSerializer.new(@integration).as_json.merge(last_failure: last_failure),
      providers: Integration::PROVIDERS.index_with(&:titleize),
      can_manage: !@integration.factory? || admin?,
    }
  end

  # The provider does not change: the Edit page shows no provider field,
  # and a Factory Integration keeps its sender.
  def update
    if @integration.update(integration_params)
      redirect_to integrations_path(org_slug: @current_org.slug), notice: "Integration updated successfully."
    else
      redirect_to edit_integration_path(@integration, org_slug: @current_org.slug), inertia: { errors: @integration.errors.to_hash }
    end
  end

  # Console deletes its own record first, with the Integration's outbox
  # rows, and then a Factory Integration's sender. Thus a local failure
  # never leaves an Integration with no sender. When the Dispatcher cannot
  # delete the sender, Console warns that it stays in the CLI's list of
  # senders.
  def destroy
    @integration.destroy!
    warning = delete_factory_sender if @integration.factory?
    redirect_to integrations_path(org_slug: @current_org.slug), notice: "Integration deleted.", alert: warning
  end

  private

  def set_integration
    @integration = @current_org.integrations.find(params[:id])
  end

  def admin?
    @current_org.organization_users.kept.find_by(user: current_user)&.admin? || false
  end

  def admin_for_factory
    head :forbidden if @integration.factory? && !admin?
  end

  def providers_to_make
    providers = Integration.providers_to_make
    admin? ? providers : providers - [ Integration::FACTORY ]
  end

  def refuse_new(errors)
    redirect_to new_integration_path(org_slug: @current_org.slug), inertia: { errors: errors }
  end

  # Makes the sender at the Dispatcher, then the record. With no Dispatcher
  # URL, or when the Dispatcher does not make the sender, there is no
  # Integration and the form shows the reason.
  def make_factory_integration
    unless Dispatcher.on?
      return refuse_new(provider: [ "needs the Dispatcher URL, which an Instance admin sets in Settings → Instance" ])
    end

    sender = FactorySender.new(user: current_user, organization: @current_org)
    @integration.sender_id, @integration.sender_secret = sender.create(@integration.name)
    unless @integration.save
      delete_orphan(sender)
      return refuse_new(@integration.errors.to_hash)
    end
    redirect_to integrations_path(org_slug: @current_org.slug), notice: "Integration created successfully."
  rescue Dispatcher::Refused, Dispatcher::Unavailable => e
    refuse_new(provider: [ "The Dispatcher did not make the sender: #{e.message}" ])
  end

  def delete_orphan(sender)
    sender.delete(@integration.sender_id)
  rescue Dispatcher::Refused, Dispatcher::Unavailable => e
    Rails.logger.warn("Console could not delete the unused sender #{@integration.sender_id}: #{e.message}")
  end

  # nil when the sender is deleted, else the warning to show.
  def delete_factory_sender
    return if @integration.sender_id.blank?

    FactorySender.new(user: current_user, organization: @current_org).delete(@integration.sender_id)
    nil
  rescue Dispatcher::Refused, Dispatcher::Unavailable => e
    id = @integration.sender_id
    "Console deleted the Integration, but could not delete its sender #{id} at the Dispatcher: #{e.message} " \
      "The sender stays in `solidtrace senders list`; an Admin deletes it with `solidtrace senders delete #{id}`."
  end

  # The Integration's last delivery that failed, with the reason, or nil.
  def last_failure
    row = Notification.last_failure(@integration)
    row && { at: row.updated_at.iso8601, reason: row.error_message, retried: row.retried? }
  end

  def integration_params
    params.require(:integration).permit(
      :name,
      :active,
      settings: [
        :webhook_url,
        :routing_key,
        :severity,
        :recipients,
        :notify_on_new_issue,
        :notify_on_assignment,
        :notify_on_event_threshold,
        :event_threshold,
        :time_window_minutes
      ]
    )
  end
end
