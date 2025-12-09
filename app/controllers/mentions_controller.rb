class MentionsController < ApplicationController
  def index
    @current_org = Current.user.organizations.find_by!(slug: params[:org_slug])
    @users = @current_org.users

    # Support query filtering for remote-filtering mode
    if params[:query].present?
      @users = @users.where("name LIKE ? OR email LIKE ?", "%#{params[:query]}%", "%#{params[:query]}%")
    end

    respond_to do |format|
      format.html { render layout: false }
      format.json
    end
  end
end
