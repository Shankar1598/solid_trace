# frozen_string_literal: true

module Oauth
  # One approval by a user of an OAuth client, for one Organization and some
  # Scopes, with the resources of those Scopes. Its OAuth tokens work only
  # while it is live (#live?).
  class Grant < ApplicationRecord
    # last_used_at is written at most this often, as an API token's is.
    TOUCH_EVERY = 1.minute

    belongs_to :user
    belongs_to :client, class_name: "Oauth::Client", foreign_key: :oauth_client_id, inverse_of: :grants
    belongs_to :organization
    has_many :codes, class_name: "Oauth::Code", foreign_key: :oauth_grant_id, dependent: :delete_all, inverse_of: :grant
    has_many :tokens, class_name: "Oauth::Token", foreign_key: :oauth_grant_id, dependent: :delete_all, inverse_of: :grant

    validates :approved_via, inclusion: { in: %w[browser device] }

    # A live Grant is not revoked, its user is still a member of its
    # Organization, and its resources are under the current issuer.
    def live?(issuer: Issuer.current)
      revoked_at.nil? && membership.present? && under_issuer?(issuer)
    end

    # The user's membership of the Grant's Organization, or nil.
    def membership
      user.organization_users.find_by(organization_id: organization_id)
    end

    # The Grants of the relation that the user sees as Signed-in clients, as
    # user.oauth_grants.signed_in_clients: live, and still have a refresh
    # token that works, newest first. A Grant with a refresh token has had a
    # token; a Grant not used for Token::REFRESH_LASTS expired and cannot act
    # any more. #live? decides; where(revoked_at: nil) only stops the load of
    # revoked Grants, whose tokens stay for CleanupJob::KEEP_TOKENS.
    def self.signed_in_clients(issuer: Issuer.current)
      working = Token.refresh.where(used_at: nil, revoked_at: nil).where(Token.arel_table[:expires_at].gt(Time.current))
      where(revoked_at: nil).where(id: working.select(:oauth_grant_id))
        .includes(:client, :organization, :user).order(approved_at: :desc, id: :desc)
        .select { |grant| grant.live?(issuer: issuer) }
    end

    # Revokes every Grant in the relation that is not revoked yet, as
    # user.oauth_grants.revoke_all!.
    def self.revoke_all!
      where(revoked_at: nil).update_all(revoked_at: Time.current)
    end

    def revoke!
      update_column(:revoked_at, Time.current) if revoked_at.nil?
    end

    # Records a use, unless one was recorded in the last minute.
    def touch_last_used
      now = Time.current
      return if last_used_at && last_used_at > now - TOUCH_EVERY

      update_column(:last_used_at, now)
    end

    private

    def under_issuer?(issuer)
      root = Issuer.normalize_resource(issuer.url)
      root.present? && resources.present? &&
        resources.all? { |resource| Issuer.normalize_resource(resource).to_s.start_with?("#{root}/") }
    end
  end
end
