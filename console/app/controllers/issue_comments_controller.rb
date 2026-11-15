# frozen_string_literal: true

# Comments on Issues over the API, for a token with issues:comment, such as
# the Dispatcher's cause report for an Issue Run. A comment from the
# Dispatcher's token, or from one of its child tokens, has "Factory" and
# the Run as its author, not the Admin who made the token (Comment). A
# comment from another token with issues:comment is by the token's user,
# as on the Issue page. Like McpController, no Session and no CSRF.
#
# POST /api/issue_comments takes JSON: project (the Project's slug in the
# token's Organization), issue (its number), body (plain text) and, for
# a Factory comment, run_id. It answers 201 with comment: id, author
# ({name, run: {id, url}} for Factory, {name} for a member), body,
# created_at and url (the comment on the Issue page). 422 {errors: {field:
# [...]}} for a field it refuses; 404 for an Issue that is not in the
# token's Organization (or its Project limit); 401 invalid_token; 403
# insufficient_scope for a token without issues:comment; 403
# two_factor_required when the Organization requires Two-factor and the
# token's user has none.
class IssueCommentsController < ActionController::API
  # The longest body, in characters.
  MAX_BODY = 65_536

  UNAUTHORIZED = "Send a token with issues:comment as Authorization: Bearer st_…. This token is unknown, revoked " \
                 "or expired, or its user left the Organization."
  NO_SCOPE = "Adding a comment to an Issue needs the issues:comment scope."
  TWO_FACTOR = "The Organization requires Two-factor, and the token's user has none. They set up Two-factor in " \
               "Settings → User."

  before_action :authenticate_token!

  def create
    body = params[:body]
    run_id = params[:run_id]
    errors = {}
    errors[:body] = [ "Give the comment's body as text." ] unless body.is_a?(String) && body.strip.present?
    errors[:body] = [ "The body can have at most #{MAX_BODY} characters." ] if body.is_a?(String) && body.length > MAX_BODY
    if factory?
      errors[:run_id] = [ "Give the Run's id as text." ] unless run_id.is_a?(String)
    elsif !run_id.nil?
      errors[:run_id] = [ "Only the Dispatcher's token links a comment to a Run." ]
    end
    return render json: { errors: errors }, status: :unprocessable_content if errors.any?

    issue = find_issue
    return render json: { error: "No such Issue in #{@bearer.organization.slug}." }, status: :not_found unless issue

    author = factory? ? { run_id: run_id } : { organization_user: issue.project.organization.organization_users.find_by!(user: @bearer.user) }
    comment = issue.comments.create!(content: Comment.html_of(body.strip), **author)
    render json: { comment: comment_json(comment, issue) }, status: :created
  rescue ActiveRecord::RecordInvalid => e
    render json: { errors: e.record.errors.to_hash }, status: :unprocessable_content
  end

  # The token's user, for the request's log line (RequestLog).
  def current_user
    @bearer&.user
  end

  private

  # The caller must be a live token with issues:comment that acts in its
  # Organization (not refused there for Require two-factor).
  def authenticate_token!
    secret = request.authorization.to_s[/\ABearer +(\S+)\z/i, 1]
    @bearer = BearerToken.authenticate(secret)
    unless @bearer
      response.set_header("WWW-Authenticate", %(Bearer error="invalid_token"))
      return render json: { error: "invalid_token", error_description: UNAUTHORIZED }, status: :unauthorized
    end
    unless @bearer.scopes.include?(Oauth::Scopes::ISSUES_COMMENT) && @bearer.organization
      response.set_header("WWW-Authenticate", %(Bearer error="insufficient_scope", scope="#{Oauth::Scopes::ISSUES_COMMENT}"))
      return render json: { error: "insufficient_scope", scope: Oauth::Scopes::ISSUES_COMMENT, error_description: NO_SCOPE },
                    status: :forbidden
    end
    if @bearer.memberships.empty?
      return render json: { error: "two_factor_required", error_description: TWO_FACTOR }, status: :forbidden
    end

    @bearer.record_use
  end

  # Whether the token is the Dispatcher's, or one of its children: its
  # comments are by Factory.
  def factory?
    token = @bearer.api_token
    token.present? && (token.dispatcher? || token.parent&.dispatcher? || false)
  end

  # The Issue that project and issue name, in the token's Organization and
  # in its Project limit, if it has one.
  def find_issue
    project = @bearer.organization.projects.find_by(slug: params[:project].to_s)
    return if project.nil? || (@bearer.project && project != @bearer.project)

    project.issues.find_by(number: params[:issue].to_s)
  end

  def comment_json(comment, issue)
    links = McpTools::Links.new(PublicUrls.console_url(request.base_url))
    author = if comment.factory?
      { name: Comment::FACTORY, run: { id: comment.run_id, url: links.run(issue.project.organization, comment.run_id) } }
    else
      { name: comment.user.name }
    end
    { id: comment.id, author: author, body: comment.content.to_plain_text, created_at: comment.created_at.iso8601,
      url: "#{links.issue(issue)}#comment-#{comment.id}" }
  end
end
