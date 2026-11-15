# frozen_string_literal: true

# Child tokens, for the Dispatcher (GLOSSARY: API token). The Dispatcher
# sends the Dispatcher's token as Authorization: Bearer, and makes a child
# token for one Execution, or revokes one of its children at the
# Execution's end. ApiToken.issue_child! refuses a child that is wider than
# its parent. Like McpController, no Session and no CSRF.
#
# POST /api/child_tokens takes JSON: name, scopes, expires_at, and the
# limits agent_session_ids and project_id. It answers 201 with api_token
# (as the API tokens page lists it) and secret, which is answered only
# here. DELETE /api/child_tokens/:id answers 204, or 404 for a token that
# is not a child of the caller.
class ChildTokensController < ActionController::API
  include ApiTokenFields

  UNAUTHORIZED = "Send the Dispatcher's token as Authorization: Bearer st_…. An Admin makes it in " \
                 "Settings → API tokens & MCP. This token is unknown, revoked or expired, or its Admin left " \
                 "the Organization."
  TWO_FACTOR = "The Organization requires Two-factor, and the Admin of the Dispatcher's token has none. The " \
               "Admin sets up Two-factor in Settings → User."
  NO_SCOPE = "Only the Dispatcher's token (tokens:create) makes and revokes child tokens."

  before_action :authenticate_parent!

  def create
    child, secret = ApiToken.issue_child!(
      parent: @parent, name: scalar(params, :name).presence || "Child of #{@parent.name}",
      scopes: list(params, :scopes) || [], expires_at: scalar(params, :expires_at),
      agent_session_ids: list(params, :agent_session_ids), project_id: scalar(params, :project_id)
    )
    render json: { api_token: ApiTokenSerializer.new(child).as_json, secret: secret }, status: :created
  rescue ActiveRecord::RecordInvalid => e
    render json: { errors: e.record.errors.to_hash }, status: :unprocessable_content
  rescue ArgumentError => e
    render json: { errors: { base: [ e.message ] } }, status: :unprocessable_content
  end

  def destroy
    child = @parent.children.find_by(id: params[:id])
    return render json: { error: "That token is not a child token of this token." }, status: :not_found unless child

    child.destroy!
    head :no_content
  end

  # The caller's user, for the request's log line (RequestLog).
  def current_user
    @parent&.user
  end

  private

  # The caller must be a live Dispatcher's token that acts in its
  # Organization (not refused there for Require two-factor).
  def authenticate_parent!
    secret = request.authorization.to_s[/\ABearer +(\S+)\z/i, 1]
    bearer = BearerToken.authenticate(secret)
    token = bearer&.api_token
    unless token
      response.set_header("WWW-Authenticate", %(Bearer error="invalid_token"))
      return render json: { error: "invalid_token", error_description: UNAUTHORIZED }, status: :unauthorized
    end
    unless token.dispatcher? && token.parent_id.nil?
      response.set_header("WWW-Authenticate", %(Bearer error="insufficient_scope", scope="tokens:create"))
      return render json: { error: "insufficient_scope", scope: Oauth::Scopes::TOKENS_CREATE, error_description: NO_SCOPE },
                    status: :forbidden
    end
    if bearer.memberships.empty?
      return render json: { error: "two_factor_required", error_description: TWO_FACTOR }, status: :forbidden
    end

    bearer.record_use
    @parent = token
  end
end
