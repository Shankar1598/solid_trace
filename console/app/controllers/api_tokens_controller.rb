# frozen_string_literal: true

# A user's own API tokens, made and revoked from Settings → API tokens & MCP
# (ApiTokenSettingsController).
class ApiTokensController < ApplicationController
  # Answers JSON with the token and its secret. The secret is answered only
  # here, and never stored.
  def create
    token, secret = ApiToken.issue!(user: current_user, name: params.require(:api_token)[:name])
    render json: { api_token: ApiTokenSerializer.new(token).as_json, secret: secret }, status: :created
  rescue ActiveRecord::RecordInvalid => e
    render json: { errors: e.record.errors.to_hash }, status: :unprocessable_content
  end

  # Revoking deletes the row, so the token no longer signs anything in.
  def destroy
    current_user.api_tokens.find(params[:id]).destroy!
    redirect_back_or_to tokens_page, notice: "API token revoked"
  end

  private

  # The page lives under an Organization; a user in none has no page to show.
  def tokens_page
    organization = home_organization
    organization ? api_token_settings_path(org_slug: organization.slug) : root_path
  end
end
