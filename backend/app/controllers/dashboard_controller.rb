class DashboardController < ApplicationController
  def index
    @projects = current_user.organizations.first&.projects || []
    # If user has no organization, we might need to handle that, but for now assuming one exists or empty list
  end
end
