# frozen_string_literal: true

class HomeController < ApplicationController
  # The first Organization's Issues, or making one for a user in none. A
  # notice from sign-up or sign-in is kept for the page it lands on.
  def index
    flash.keep
    if current_user.organizations.any?
      redirect_to issues_path(org_slug: current_user.organizations.first.slug)
    else
      redirect_to new_organization_path
    end
  end
end
