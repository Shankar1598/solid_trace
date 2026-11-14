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
    # user.oauth_grants.signed_in_clients: live, and have had a token,
    # newest first.
    def self.signed_in_clients(issuer: Issuer.current)
      where(revoked_at: nil).where.not(first_token_at: nil)
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
