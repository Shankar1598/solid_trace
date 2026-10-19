# frozen_string_literal: true

# The Organization a page is under, found by the URL's slug among the
# signed-in user's Organizations, so another Organization's slug is a 404.
# Opening it makes it the last used Organization, which `/` opens.
module CurrentOrganization
  extend ActiveSupport::Concern

  private

  def set_current_org
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
    session[:last_organization_id] = @current_org.id
  end

  # The last used Organization while the user is still in it, otherwise their
  # first by name, or nil for a user in none.
  def home_organization
    current_user.organizations.find_by(id: session[:last_organization_id]) ||
      current_user.organizations.order(:name).first
  end
end
