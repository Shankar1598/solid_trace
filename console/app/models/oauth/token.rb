# frozen_string_literal: true

module Oauth
  # An OAuth token under a Grant: an access token (sto_, 1 hour, for one
  # resource) or a refresh token (str_, 30 days). Console keeps only the
  # SHA-256 digest of its secret. Neither prefix starts with st_, so an API
  # token check never takes one.
  class Token < ApplicationRecord
    ACCESS_PREFIX = "sto_"
    REFRESH_PREFIX = "str_"
    ACCESS_LASTS = 1.hour
    REFRESH_LASTS = 30.days

    belongs_to :grant, class_name: "Oauth::Grant", foreign_key: :oauth_grant_id, inverse_of: :tokens

    validates :kind, inclusion: { in: %w[access refresh] }

    scope :access, -> { where(kind: "access") }

    # Makes an access token for resource and a refresh token under grant,
    # and returns their secrets.
    def self.issue_pair!(grant:, resource:)
      access = Secret.generate(ACCESS_PREFIX)
      refresh = Secret.generate(REFRESH_PREFIX)
      now = Time.current
      transaction do
        create!(kind: "access", token_digest: Secret.digest(access), grant: grant, resource: resource,
                expires_at: now + ACCESS_LASTS)
        create!(kind: "refresh", token_digest: Secret.digest(refresh), grant: grant, expires_at: now + REFRESH_LASTS)
        grant.update_column(:first_token_at, now) if grant.first_token_at.nil?
      end
      { access_token: access, refresh_token: refresh, expires_in: ACCESS_LASTS.to_i }
    end

    # The live access token whose secret this is: not expired, not revoked,
    # under a live Grant. Nil otherwise.
    def self.find_live_access(secret, issuer:)
      return unless secret.is_a?(String) && secret.start_with?(ACCESS_PREFIX)

      token = access.includes(grant: [ :user, :organization ]).find_by(token_digest: Secret.digest(secret))
      token if token && token.revoked_at.nil? && token.expires_at > Time.current && token.grant.live?(issuer: issuer)
    end
  end
end
