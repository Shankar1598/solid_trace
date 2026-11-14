# frozen_string_literal: true

# A user's own API tokens, made and revoked from Settings → API tokens & MCP
# (ApiTokenSettingsController).
class ApiTokensController < ApplicationController
  # One value of a form field: a String, a number or nothing.
  SCALAR = [ String, Integer, NilClass ].freeze

  # Answers JSON with the token and its secret. The secret is answered only
  # here, and never stored. Each field goes to ApiToken as it was sent, so
  # a field in the wrong shape is refused, never dropped: a dropped Scope or
  # limit would make a wider token than the one asked for.
  def create
    fields = params.require(:api_token)
    token, secret = ApiToken.issue!(
      user: current_user, name: scalar(fields, :name),
      scopes: list(fields, :scopes) || [], agent_session_ids: list(fields, :agent_session_ids),
      organization_id: scalar(fields, :organization_id), expires_at: scalar(fields, :expires_at),
      project_id: scalar(fields, :project_id)
    )
    render json: { api_token: ApiTokenSerializer.new(token).as_json, secret: secret }, status: :created
  rescue ActiveRecord::RecordInvalid => e
    render json: { errors: e.record.errors.to_hash }, status: :unprocessable_content
  rescue ArgumentError => e
    render json: { errors: { base: [ e.message ] } }, status: :unprocessable_content
  end

  # Revoking deletes the row, so the token no longer signs anything in.
  def destroy
    current_user.api_tokens.find(params[:id]).destroy!
    redirect_back_or_to tokens_page, notice: "API token revoked"
  end

  private

  def scalar(fields, key)
    value = fields[key]
    raise ArgumentError, "Give #{key} as one value." unless SCALAR.any? { |kind| value.is_a?(kind) }

    value
  end

  # A list field: nil, or an Array whose items are each one value (an item
  # in another shape becomes a String that ApiToken refuses).
  def list(fields, key)
    value = fields[key]
    return if value.nil?
    raise ArgumentError, "Give #{key} as a list." unless value.is_a?(Array)

    value.map { |item| SCALAR.any? { |kind| item.is_a?(kind) } ? item : item.to_s }
  end

  # The page lives under an Organization; a user in none has no page to show.
  def tokens_page
    organization = home_organization
    organization ? api_token_settings_path(org_slug: organization.slug) : root_path
  end
end
