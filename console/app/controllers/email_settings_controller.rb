# frozen_string_literal: true

# Settings → Email: the SMTP server and the sender Console sends every
# Organization's email with. They're the install's, so only an instance admin
# sees or changes them (ADR 0015). The password is write-only.
class EmailSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org
  before_action :require_instance_admin

  def show
    render inertia: "Settings/Email", props: {
      settings: EmailSettingsSerializer.new(InstanceSetting.current).as_json,
    }
  end

  def update
    settings = InstanceSetting.current
    if settings.update_email(settings_params.except(:clear_password),
        clear_password: ActiveModel::Type::Boolean.new.cast(settings_params[:clear_password]))
      redirect_to email_settings_path(org_slug: @current_org.slug), notice: "Email settings saved"
    else
      redirect_to email_settings_path(org_slug: @current_org.slug), inertia: { errors: settings.errors.to_hash }
    end
  end

  # Sends a test email at once, so the SMTP server's error, if any, is shown.
  def send_test
    to = params[:to].to_s.strip.presence || current_user.email
    failure = EmailSettingsMailer.send_test(to)
    if failure
      redirect_to email_settings_path(org_slug: @current_org.slug), alert: "The test email wasn't sent: #{failure}"
    else
      redirect_to email_settings_path(org_slug: @current_org.slug), notice: "Test email sent to #{to}"
    end
  end

  private

  def settings_params
    params.require(:email_settings).permit(:smtp_address, :smtp_port, :smtp_username, :smtp_password, :mail_from,
      :clear_password)
  end
end
