# frozen_string_literal: true

class HomeController < ApplicationController
  def index
    if current_user.organizations.any?
      redirect_to issues_path(org_slug: current_user.organizations.first.slug)
    else
      # Redirect to a "create org" page or show a welcome message
      render plain: "Welcome! Please create an organization."
    end
  end
end
