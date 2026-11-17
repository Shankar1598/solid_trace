# frozen_string_literal: true

module Oauth
  # An OAuth token under a Grant: an access token (sto_, 1 hour, for one
  # resource) or a refresh token (str_, 30 days). Console keeps only the
  # SHA-256 digest of its secret. Neither prefix starts with st_, so an API
  # token check never takes one.
  #
  # A refresh token works once (#rotate!). Its first use makes a new pair,
  # and the used token keeps that pair's secrets, encrypted, for
  # REUSE_WINDOW: a second use in the window gets the same pair again, so a
  # Grant has one chain of refresh tokens. ClearKeptPairJob clears the
  # secrets after the window, and a use after the window never reads them.
  class Token < ApplicationRecord
    ACCESS_PREFIX = "sto_"
    REFRESH_PREFIX = "str_"
    ACCESS_LASTS = 1.hour
    REFRESH_LASTS = 30.days
    REUSE_WINDOW = 60.seconds

    belongs_to :grant, class_name: "Oauth::Grant", foreign_key: :oauth_grant_id, inverse_of: :tokens
    belongs_to :replaced_by, class_name: "Oauth::Token", optional: true

    encrypts :kept_access_token, :kept_refresh_token

    validates :kind, inclusion: { in: %w[access refresh] }

    scope :access, -> { where(kind: "access") }
    scope :refresh, -> { where(kind: "refresh") }

    # Makes an access token for resource and a refresh token under grant,
    # and returns their secrets.
    def self.issue_pair!(grant:, resource:)
      make_pair(grant: grant, resource: resource).first
    end

    # The live access token whose secret this is: not expired, not revoked,
    # under a live Grant. Nil otherwise.
    def self.find_live_access(secret, issuer:)
      return unless secret.is_a?(String) && secret.start_with?(ACCESS_PREFIX)

      token = access.includes(grant: [ :user, :organization ]).find_by(token_digest: Secret.digest(secret))
      token if token && token.revoked_at.nil? && token.expires_at > Time.current && token.grant.live?(issuer: issuer)
    end

    # The refresh token whose secret this is, used or not, or nil.
    def self.find_refresh(secret)
      return unless secret.is_a?(String) && secret.start_with?(REFRESH_PREFIX)

      refresh.includes(:grant).find_by(token_digest: Secret.digest(secret))
    end

    # The OAuth token, access or refresh, whose secret this is, or nil.
    def self.find_by_secret(secret)
      return unless secret.is_a?(String) && secret.start_with?(ACCESS_PREFIX, REFRESH_PREFIX)

      includes(:grant).find_by(token_digest: Secret.digest(secret))
    end

    # [the secrets, the refresh token's row]. The access token expires
    # ACCESS_LASTS after now.
    def self.make_pair(grant:, resource:, now: Time.current)
      access = Secret.generate(ACCESS_PREFIX)
      refresh = Secret.generate(REFRESH_PREFIX)
      transaction do
        create!(kind: "access", token_digest: Secret.digest(access), grant: grant, resource: resource,
                expires_at: now + ACCESS_LASTS)
        row = create!(kind: "refresh", token_digest: Secret.digest(refresh), grant: grant, expires_at: now + REFRESH_LASTS)
        grant.update_column(:first_token_at, now) if grant.first_token_at.nil?
        [ { access_token: access, refresh_token: refresh, expires_in: ACCESS_LASTS.to_i }, row ]
      end
    end

    def expired?
      expires_at <= Time.current
    end

    # The new pair for this refresh token, with an access token for
    # resource. The first use makes it with one conditional update, so of
    # parallel uses only one makes a pair, and the others get that pair. A
    # use in the REUSE_WINDOW after the first gets the same pair again. Nil
    # for a use after the window: the caller then revokes the Grant.
    def rotate!(resource:)
      return kept_pair if used_at

      now = Time.current
      # requires_new, so that a lost race rolls back its pair also when a
      # caller has a transaction open.
      self.class.transaction(requires_new: true) do
        pair, replacement = self.class.make_pair(grant: grant, resource: resource, now: now)
        raise ActiveRecord::Rollback unless self.class.where(id: id, used_at: nil).update_all(used_at: now) == 1

        update!(used_at: now, replaced_by: replacement, kept_access_token: pair[:access_token],
                kept_refresh_token: pair[:refresh_token])
      end
      ClearKeptPairJob.set(wait: REUSE_WINDOW + 1.second).perform_later(id) if replaced_by_id

      reload.kept_pair
    end

    # The pair that the first use made, while the window is open; else nil.
    def kept_pair
      return unless used_at && used_at > REUSE_WINDOW.ago && kept_access_token && kept_refresh_token

      { access_token: kept_access_token, refresh_token: kept_refresh_token,
        expires_in: (used_at + ACCESS_LASTS - Time.current).round }
    end
  end
end
