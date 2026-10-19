# frozen_string_literal: true

class UserSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org, if: -> { params[:org_slug] }

  def show
    render inertia: "Settings/User", props: {
      user: UserSerializer.new(current_user).as_json,
      api_tokens: current_user.api_tokens.order(created_at: :desc, id: :desc).map { |t| ApiTokenSerializer.new(t).as_json },
      # Where a client adds the MCP server, as this request reaches Console.
      mcp_url: "#{request.base_url}/mcp",
    }
  end

  def update
    if current_user.update(user_params)
      redirect_back_or_to settings_page, notice: "User settings updated"
    else
      redirect_back_or_to settings_page, inertia: { errors: current_user.errors.to_hash }
    end
  end

  private

  def user_params
    params.require(:user).permit(:name)
  end

  # The page is served with and without an Organization.
  def settings_page
    @current_org ? user_path(org_slug: @current_org.slug) : user_settings_path
  end
end
