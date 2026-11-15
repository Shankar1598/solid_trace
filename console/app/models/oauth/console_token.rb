# frozen_string_literal: true

module Oauth
  # Console's factory token: the access token that Console's server makes
  # for the signed-in user when it calls the Dispatcher for them (the Stop
  # button, the Factory section, the Factory Integration). It has only the
  # factory Scope, acts in one Organization of the user, and works for
  # LASTS from when it is made. There is no refresh.
  #
  # It is not stored. The secret is the user, the Organization and the
  # times, signed with a key from secret_key_base, after PREFIX. So it is
  # not an API token or an OAuth token: EventStore, which reads only their
  # tables, never accepts it, and the API tokens page never lists it. The
  # check (BearerToken) reads the membership each time, so it stops at once
  # when the user leaves the Organization.
  class ConsoleToken
    PREFIX = "stc_"
    LASTS = 5.minutes
    SCOPES = [ "factory" ].freeze
    PURPOSE = "console factory token"

    attr_reader :user, :organization, :issued_at, :expires_at

    # The secret of a new token for user in organization, one of the user's
    # Organizations. Raises ArgumentError for any other Organization.
    def self.issue(user:, organization:, now: Time.current)
      unless user.organization_users.exists?(organization_id: organization.id)
        raise ArgumentError, "#{user.email} is not a member of #{organization.slug}"
      end

      expires_at = now + LASTS
      payload = { "user" => user.id, "organization" => organization.id, "iat" => now.to_i, "exp" => expires_at.to_i }
      PREFIX + verifier.generate(payload, expires_at: expires_at, purpose: PURPOSE)
    end

    # The live token whose secret this is: signed by Console, not expired,
    # and its user still a member of its Organization. Nil otherwise.
    def self.find_live(secret)
      return unless secret.is_a?(String) && secret.start_with?(PREFIX)

      payload = verifier.verified(secret.delete_prefix(PREFIX), purpose: PURPOSE)
      return unless payload.is_a?(Hash)

      membership = OrganizationUser.kept.includes(:user, :organization)
        .find_by(user_id: payload["user"], organization_id: payload["organization"])
      return unless membership

      new(user: membership.user, organization: membership.organization,
          issued_at: Time.zone.at(payload["iat"]), expires_at: Time.zone.at(payload["exp"]))
    end

    def self.verifier
      @verifier ||= ActiveSupport::MessageVerifier.new(
        Rails.application.key_generator.generate_key(PURPOSE, 64), digest: "SHA256", serializer: JSON, url_safe: true,
      )
    end
    private_class_method :verifier

    def initialize(user:, organization:, issued_at:, expires_at:)
      @user = user
      @organization = organization
      @issued_at = issued_at
      @expires_at = expires_at
    end

    def organization_id
      organization.id
    end

    # A Console token is not stored, so no use is recorded.
    def touch_last_used; end
  end
end
