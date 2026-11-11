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
    return send_to_set_up_two_factor if @current_org.require_two_factor && !current_user.two_factor?

    remember_last_organization if page_opened?
  end

  # Require two-factor: a member without Two-factor goes to the set-up page,
  # which names the Organization and returns there once it's set up. Asked
  # on every request, so it applies at once to Sessions already signed in.
  # A request that isn't a browser's, such as a poll, is refused.
  def send_to_set_up_two_factor
    if request.inertia? || (request.format.html? && !request.xhr?)
      redirect_to new_authenticator_app_path(org: @current_org.slug)
    else
      head :forbidden
    end
  end

  def remember_last_organization
    session[:last_organization_id] = @current_org.id
  end

  # A GET for a page: an Inertia visit, or a plain HTML load.
  def page_opened?
    request.get? && (request.inertia? || (request.format.html? && !request.xhr?))
  end

  # The last used Organization while the user is still in it, otherwise their
  # first by name, or nil for a user in none; of those they can open first,
  # passing over any whose Require two-factor they don't meet.
  def home_organization
    candidates = current_user.organizations_by_name.to_a
      .partition { |organization| organization.id == session[:last_organization_id] }.flatten
    return candidates.first if current_user.two_factor?

    candidates.find { |organization| !organization.require_two_factor } || candidates.first
  end
end
