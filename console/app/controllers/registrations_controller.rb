# frozen_string_literal: true

# Sign-up, while InstanceSetting#sign_up_allowed?: on an install that has a
# user, only once the operator has opened it. Otherwise people join by Invite.
class RegistrationsController < ApplicationController
  CLOSED = "Ask an admin of your Organization to invite you."

  layout "auth"
  skip_before_action :authenticate_user!, only: [ :new, :create ]
  rate_limit to: 10, within: 3.minutes, only: :create,
    with: -> { redirect_to new_registration_path, alert: "Too many tries. Try again in a few minutes." }
  before_action :require_sign_up_allowed

  def new
    render inertia: "Registrations/New"
  end

  def create
    # Whoever signs up first set up the install, so they change its settings.
    @user = User.new(user_params.merge(instance_admin: !User.exists?))
    if @user.save
      MetaOrganization.join_first(@user)
      start_new_session_for @user
      redirect_to root_path, notice: "Welcome! You have signed up successfully."
    else
      redirect_to new_registration_path, inertia: { errors: @user.errors.to_hash }
    end
  end

  private

  def require_sign_up_allowed
    return if InstanceSetting.current.sign_up_allowed?

    if request.get?
      render inertia: "Registrations/New", props: { closed: CLOSED }
    else
      redirect_to new_registration_path
    end
  end

  def user_params
    params.require(:user).permit(:name, :email, :password, :password_confirmation)
  end
end
