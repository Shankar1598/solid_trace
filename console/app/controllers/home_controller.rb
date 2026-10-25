# frozen_string_literal: true

class HomeController < ApplicationController
  # The last used Organization's Overview, or making one for a user in none. A
  # notice from sign-up or sign-in is kept for the page it lands on.
  def index
    flash.keep
    if (organization = home_organization)
      redirect_to overview_path(org_slug: organization.slug)
    else
      redirect_to new_organization_path
    end
  end
end
