class ApplicationController < ActionController::Base
  helper_method :current_user
  before_action :authenticate_user!

  private

  def current_user
    @current_user ||= User.first
  end

  def current_organization
    @current_organization ||= if params[:org_slug]
                                Organization.find_by(slug: params[:org_slug])
                              else
                                current_user&.organizations&.first
                              end
  end
  helper_method :current_organization

  def authenticate_user!
    current_user
  end
end
