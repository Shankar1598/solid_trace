# frozen_string_literal: true

# A secret a user makes in their settings that lets a client act as that
# user (GLOSSARY: API token). The secret is shown once when the token is
# made; Console keeps only its SHA-256 digest and finds the token by it.
#
# A token with no Scope acts as its user over the MCP server, as all tokens
# did before Scopes. A token with Scopes (Oauth::Scopes) can do only what
# they give, in its one Organization. It can have limits:
# - agent_session_ids: the Agent sessions that sessions:write and
#   sessions:read reach; sessions:read always has the list;
# - project: the one Project that mcp:read reads.
# A token with an expiry stops working at that time. EventStore reads these
# columns for the agent ingest API.
#
# The Dispatcher's token is the one token with tokens:create: an Admin makes
# it with Oauth::Scopes::DISPATCHER and no limit. It makes child tokens
# (issue_child!) for its Admin in its Organization, each with some of the
# Scopes it can give, the limits that go with them and an expiry no later
# than its own. A child works only while its parent works, and revoking the
# parent deletes each child (the foreign key on parent_id).
class ApiToken < ApplicationRecord
  PREFIX = "st_"
  SECRET_BYTES = 32
  # last_used_at is written at most this often, so a busy client doesn't take
  # SQLite's write lock on every call.
  TOUCH_EVERY = 1.minute
  # An Agent session id: the harness's id for it, such as a UUID. No dots or
  # slashes, as EventStore puts it in paths.
  AGENT_SESSION_ID = /\A[A-Za-z0-9][A-Za-z0-9_-]{0,127}\z/
  MAX_AGENT_SESSION_IDS = 100

  belongs_to :user
  belongs_to :organization, optional: true
  belongs_to :project, optional: true
  belongs_to :parent, class_name: "ApiToken", optional: true, inverse_of: :children
  has_many :children, class_name: "ApiToken", foreign_key: :parent_id, inverse_of: :parent, dependent: :delete_all

  validates :name, presence: true, length: { maximum: 100 }
  validates :token_digest, presence: true, uniqueness: true
  validate :scopes_in_registry, :limits_fit_scopes
  # Checked at save of a new token: later the expiry passes, or the user
  # leaves, and live? refuses the token then.
  validate :organization_of_user, :expiry_in_future, :dispatcher_of_admin, :child_fits_parent, on: :create

  # Makes a token for user and returns it with its secret, which is never
  # stored. Raises ActiveRecord::RecordInvalid when a field is refused.
  # scopes are names; organization_id, expires_at, agent_session_ids and
  # project_id are as a form sends them. agent_session_ids goes as it was
  # sent: an empty list is refused, as no list would reach every Agent
  # session.
  def self.issue!(user:, name:, scopes: [], organization_id: nil, expires_at: nil, agent_session_ids: nil, project_id: nil,
                  parent: nil)
    secret = PREFIX + SecureRandom.urlsafe_base64(SECRET_BYTES)
    token = create!(user: user, name: name.to_s.strip, token_digest: digest(secret),
                    scopes: scopes.is_a?(Array) ? scopes.uniq : scopes, organization_id: organization_id.presence,
                    expires_at: expires_at.presence, agent_session_ids: agent_session_ids,
                    project_id: project_id.presence, parent: parent)
    [ token, secret ]
  end

  # Makes a child token of parent, for parent's user in parent's
  # Organization, and returns it with its secret. It also deletes parent's
  # children that have expired, so they do not collect. Raises
  # ActiveRecord::RecordInvalid when the child would be wider than parent.
  def self.issue_child!(parent:, name:, scopes:, expires_at:, agent_session_ids: nil, project_id: nil)
    parent.children.where(expires_at: ..Time.current).delete_all
    issue!(user: parent.user, name: name, scopes: scopes, organization_id: parent.organization_id,
           expires_at: expires_at, agent_session_ids: agent_session_ids, project_id: project_id, parent: parent)
  end

  # The token whose secret this is, or nil.
  def self.find_by_secret(secret)
    return nil unless secret.is_a?(String) && secret.start_with?(PREFIX)

    find_by(token_digest: digest(secret))
  end

  def self.digest(secret)
    Digest::SHA256.hexdigest(secret)
  end

  # Whether the token works now: not expired, and its user is still a
  # member of its Organization. A revoked token has no row, and a member who
  # leaves an Organization loses its tokens (OrganizationUser).
  # A child works only while its parent works.
  def live?(now: Time.current)
    (expires_at.nil? || expires_at > now) &&
      (organization_id.nil? || user.organization_users.exists?(organization_id: organization_id)) &&
      (parent_id.nil? || parent&.live?(now: now) == true)
  end

  # Whether this is the Dispatcher's token: the one that makes child tokens.
  def dispatcher?
    scopes.is_a?(Array) && scopes.include?(Oauth::Scopes::TOKENS_CREATE)
  end

  # The Scopes that a child of this token can have: its own but
  # tokens:create, and the Scopes it gives only to children. None for a
  # token that is not the Dispatcher's.
  def scopes_for_children
    return [] unless dispatcher?

    (scopes - [ Oauth::Scopes::TOKENS_CREATE ]) | Oauth::Scopes::FOR_CHILDREN
  end

  # The Scopes the token acts with: mcp for a token with none.
  def effective_scopes
    scopes.presence || [ Oauth::Scopes::MCP ]
  end

  # Its limits, as introspection and the API tokens page give them.
  def limits
    {
      agent_session_ids: agent_session_ids.presence,
      project: project && { id: project.id, slug: project.slug, name: project.name },
    }.compact
  end

  # Records a use, unless one was recorded in the last minute.
  def touch_last_used
    now = Time.current
    return if last_used_at && last_used_at > now - TOUCH_EVERY

    update_column(:last_used_at, now)
  end

  private

  def scopes_in_registry
    return errors.add(:scopes, "Give the scopes as a list of names.") unless scopes.is_a?(Array)

    scopes.each do |name|
      scope = Oauth::Scopes.find(name)
      if scope.nil?
        errors.add(:scopes, "#{name.to_s.inspect} is not a scope. The scopes are: " \
                            "#{Oauth::Scopes.for_people.map(&:name).join(', ')}.")
      elsif !scope.person? && !(name == Oauth::Scopes::TOKENS_CREATE && parent_id.nil?)
        errors.add(:scopes, "#{name} is only for the Dispatcher's token, not for a person's own token.")
      end
    end
    dispatcher_scopes_fit
  end

  # tokens:create goes only with the Dispatcher's Scopes, and with no limit,
  # so the Dispatcher's token has one fixed shape.
  def dispatcher_scopes_fit
    return unless dispatcher? && parent_id.nil?

    if scopes.sort != Oauth::Scopes::DISPATCHER.sort
      errors.add(:scopes, "tokens:create is only for the Dispatcher's token, which has exactly these scopes: " \
                          "#{Oauth::Scopes::DISPATCHER.join(', ')}.")
    end
    if !agent_session_ids.nil? || project_id
      errors.add(:limits, "The Dispatcher's token (tokens:create) has no limit: it gives limits to its child tokens.")
    end
  end

  def dispatcher_of_admin
    return unless dispatcher? && parent_id.nil? && organization_id

    membership = user&.organization_users&.find_by(organization_id: organization_id)
    return if membership.nil? || membership.admin?

    errors.add(:scopes, "Only an Admin of #{organization.name} can make the Dispatcher's token (tokens:create).")
  end

  # A child is never wider than its parent: some of the Scopes the parent
  # can give, with their limits, in the parent's Organization, for the
  # parent's user, with an expiry no later than the parent's.
  def child_fits_parent
    return if parent.nil?
    return errors.add(:parent, "That token cannot make child tokens.") unless parent.dispatcher?

    names = scopes.is_a?(Array) ? scopes : []
    child_scopes_fit(names)
    child_limits_fit(names)
    child_expiry_fits
    if user_id != parent.user_id || organization_id != parent.organization_id
      errors.add(:parent, "A child token acts as its parent's user, in its parent's Organization.")
    end
  end

  def child_scopes_fit(names)
    # A token with no Scope acts as mcp in each Organization.
    errors.add(:scopes, "A child token needs at least one scope.") if names.empty?
    wider = names - parent.scopes_for_children
    return if wider.empty?

    errors.add(:scopes, "A child token can have only these scopes: #{parent.scopes_for_children.join(', ')}. " \
                        "Not: #{wider.join(', ')}.")
  end

  def child_limits_fit(names)
    if (names & [ "sessions:write", "sessions:read" ]).any? && agent_session_ids.nil?
      errors.add(:limits, "A child token with sessions:write or sessions:read needs a list of Agent session ids.")
    end
    return unless names.include?(Oauth::Scopes::MCP_READ)

    if project_id.nil?
      errors.add(:limits, "A child token with mcp:read needs one Project.")
    elsif project&.agent?
      errors.add(:limits, "A child token with mcp:read cannot read the Agent Project.")
    end
  end

  def child_expiry_fits
    if expires_at.nil?
      errors.add(:expires_at, "A child token needs an expiry.") if expires_at_before_type_cast.blank?
    elsif parent.expires_at && expires_at > parent.expires_at
      errors.add(:expires_at, "A child token's expiry must be no later than its parent's " \
                              "(#{parent.expires_at.utc.iso8601}).")
    end
  end

  def organization_of_user
    if organization_id.nil?
      errors.add(:organization, "A token with scopes needs one Organization.") if scopes.present?
    elsif !user&.organization_users&.exists?(organization_id: organization_id)
      errors.add(:organization, "That Organization is not one of your Organizations.")
    end
  end

  def expiry_in_future
    if expires_at.nil?
      errors.add(:expires_at, "Give the expiry as a date and time.") if expires_at_before_type_cast.present?
    elsif expires_at <= Time.current
      errors.add(:expires_at, "The expiry must be in the future.")
    end
  end

  def limits_fit_scopes
    names = scopes.is_a?(Array) ? scopes : []
    agent_session_limit_fits(names)
    project_limit_fits(names)
  end

  def agent_session_limit_fits(names)
    ids = agent_session_ids
    if ids.nil?
      errors.add(:limits, "sessions:read needs a list of Agent session ids.") if names.include?("sessions:read")
      return
    end

    if !ids.is_a?(Array) || ids.empty?
      errors.add(:limits, "Give the Agent session ids as a list.")
    elsif ids.size > MAX_AGENT_SESSION_IDS
      errors.add(:limits, "Give at most #{MAX_AGENT_SESSION_IDS} Agent session ids.")
    elsif (bad = ids.find { |id| !id.is_a?(String) || !AGENT_SESSION_ID.match?(id) })
      errors.add(:limits, "#{bad.to_s.inspect} is not an Agent session id: use letters, digits, - and _ " \
                          "(at most 128).")
    elsif (names & [ "sessions:write", "sessions:read" ]).empty?
      errors.add(:limits, "A list of Agent session ids needs sessions:write or sessions:read.")
    end
  end

  def project_limit_fits(names)
    return if project_id.nil?

    if !names.include?(Oauth::Scopes::MCP_READ)
      errors.add(:limits, "A Project limit needs mcp:read.")
    elsif names.include?(Oauth::Scopes::MCP)
      errors.add(:limits, "mcp reaches every Project: leave it out, or leave out the Project limit.")
    elsif organization.nil? || !organization.projects.exists?(id: project_id)
      errors.add(:limits, "That Project is not a Project of #{organization&.name || 'the token\'s Organization'}.")
    end
  end
end
