class OrganizationMembersController < ApplicationController
  layout "dashboard"
  before_action :set_organization

  def create
    @user = User.find_by(email: member_params[:email])
    new_user_created = false
    generated_password = nil

    if @user.nil?
      # Auto-create the user with a random password
      generated_password = SecureRandom.alphanumeric(12)
      @user = User.new(
        email: member_params[:email],
        name: member_params[:name].presence || member_params[:email].split("@").first,
        password: generated_password,
        password_confirmation: generated_password
      )

      unless @user.save
        render turbo_stream: turbo_stream.replace("member-form-errors", partial: "organization_members/form_errors", locals: { error: @user.errors.full_messages.first })
        return
      end

      new_user_created = true
    end

    @organization_user = @current_org.organization_users.build(user: @user)

    if @organization_user.save
      # Send invitation email to newly created users
      if new_user_created && generated_password
        OrganizationMailer.invitation_email(
          user: @user,
          organization: @current_org,
          password: generated_password,
          invited_by: current_user
        ).deliver_now
      end

      respond_to do |format|
        format.turbo_stream do
          render turbo_stream: [
            turbo_stream.append("members-list", partial: "organization_members/member", locals: { member: @organization_user }),
            turbo_stream.replace("member-form", partial: "organization_members/form", locals: { organization: @current_org }),
            turbo_stream.replace("member-form-errors", partial: "organization_members/form_errors", locals: { error: nil })
          ]
        end
        format.html { redirect_to settings_path(org_slug: @current_org.slug), notice: "Member added successfully" }
      end
    else
      render turbo_stream: turbo_stream.replace("member-form-errors", partial: "organization_members/form_errors", locals: { error: @organization_user.errors.full_messages.first })
    end
  end

  def destroy
    @organization_user = @current_org.organization_users.find(params[:id])

    # Prevent removing the last member
    if @current_org.organization_users.count <= 1
      flash[:alert] = "Cannot remove the last member of the organization"
      redirect_to settings_path(org_slug: @current_org.slug)
      return
    end

    @organization_user.destroy

    respond_to do |format|
      format.turbo_stream { render turbo_stream: turbo_stream.remove("member-#{@organization_user.id}") }
      format.html { redirect_to settings_path(org_slug: @current_org.slug), notice: "Member removed successfully" }
    end
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def member_params
    params.require(:member).permit(:email, :name)
  end
end
