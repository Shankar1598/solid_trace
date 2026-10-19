# frozen_string_literal: true

# The Organization a page is under, found by the URL's slug among the
# signed-in user's Organizations, so another Organization's slug is a 404.
# Opening a page of it makes it the last used Organization, which `/` opens;
# JSON and XHR requests under it, such as a poll in another tab, don't.
module CurrentOrganization
  extend ActiveSupport::Concern

  private

  def set_current_org
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
    remember_last_organization if page_opened?
  end

  def remember_last_organization
    session[:last_organization_id] = @current_org.id
  end

  # A GET for a page: an Inertia visit, or a plain HTML load.
  def page_opened?
    request.get? && (request.inertia? || (request.format.html? && !request.xhr?))
  end

  # The last used Organization while the user is still in it, otherwise their
  # first by name, or nil for a user in none.
  def home_organization
    current_user.organizations.find_by(id: session[:last_organization_id]) ||
      current_user.organizations_by_name.first
  end
end
