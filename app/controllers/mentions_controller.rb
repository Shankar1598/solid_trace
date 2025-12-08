class MentionsController < ApplicationController
  def index
    @current_org = Current.user.organizations.find_by!(slug: params[:org_slug])
    @users = @current_org.users

    respond_to do |format|
      format.json
    end
  end
end
