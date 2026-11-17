# frozen_string_literal: true

module Oauth
  # An authorization code: 32 random bytes, base64url, that last 60 seconds
  # and work once. Console keeps its SHA-256 digest, the PKCE S256
  # code_challenge, the redirect URI, the Scopes and the resource; never the
  # code itself or the request's state.
  #
  # A device code (DeviceCode) is a Code of kind device. Its client is known
  # only by client_id until a signed-in user types its user code on the
  # device page; Approve sets its Grant, Deny its denied_at.
  class Code < ApplicationRecord
    LASTS = 60.seconds

    belongs_to :client, class_name: "Oauth::Client", foreign_key: :oauth_client_id, inverse_of: :codes, optional: true
    belongs_to :grant, class_name: "Oauth::Grant", foreign_key: :oauth_grant_id, optional: true, inverse_of: :codes

    validates :kind, inclusion: { in: %w[authorization device] }
    validates :client, presence: true, unless: :client_id?

    # Makes an authorization code under grant and returns it with its secret.
    def self.issue_authorization!(grant:, code_challenge:, redirect_uri:, resource:)
      secret = Secret.generate
      code = create!(kind: "authorization", code_digest: Secret.digest(secret), client: grant.client, grant: grant,
                     code_challenge: code_challenge, redirect_uri: redirect_uri, scopes: grant.scopes,
                     resource: resource, expires_at: LASTS.from_now)
      [ code, secret ]
    end

    def self.find_by_secret(secret)
      return unless secret.is_a?(String) && secret.present?

      find_by(code_digest: Secret.digest(secret))
    end

    def expired?
      expires_at <= Time.current
    end

    # Marks the code used with one conditional update, so of two parallel
    # requests only one uses it. True for the request that used it.
    def use!
      self.class.where(id: id, used_at: nil).update_all(used_at: Time.current) == 1
    end

    # Whether the device code waits for a decision: not expired, approved,
    # denied or used.
    def waiting?
      !expired? && oauth_grant_id.nil? && denied_at.nil? && used_at.nil?
    end

    # Approves the waiting device code under grant, with one conditional
    # update, so of two decisions at once only one counts. True for that one.
    def approve!(grant)
      waiting_row.update_all(oauth_grant_id: grant.id) == 1
    end

    # Denies the waiting device code. True when this call denied it.
    def deny!
      waiting_row.update_all(denied_at: Time.current) == 1
    end

    # Records a poll of the device code. True when the program waited its
    # poll interval since the last poll; else the interval gets
    # DeviceCode::SLOW_DOWN more seconds (RFC 8628 §3.5), and false.
    def poll!
      now = Time.current
      waited = self.class.where(id: id).where("last_poll_at IS NULL OR last_poll_at <= ?", now - poll_interval.seconds)
        .update_all(last_poll_at: now)
      return true if waited == 1

      self.class.where(id: id).update_all([ "poll_interval = poll_interval + ?, last_poll_at = ?", DeviceCode::SLOW_DOWN, now ])
      false
    end

    private

    def waiting_row
      self.class.where(id: id, oauth_grant_id: nil, denied_at: nil, used_at: nil).where("expires_at > ?", Time.current)
    end
  end
end
