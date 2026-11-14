# frozen_string_literal: true

module Oauth
  # An OAuth client. A registered (DCR) client is public: it sends its
  # client_id and proves the code with PKCE. Its name is the one it gave,
  # which Console cannot check.
  class Client < ApplicationRecord
    KINDS = %w[dcr].freeze
    UNNAMED = "Unnamed client"
    MAX_URI = 512
    DEFAULT_GRANT_TYPES = %w[authorization_code refresh_token].freeze

    has_many :grants, class_name: "Oauth::Grant", foreign_key: :oauth_client_id, dependent: :destroy, inverse_of: :client
    has_many :codes, class_name: "Oauth::Code", foreign_key: :oauth_client_id, dependent: :delete_all, inverse_of: :client
    has_many :pending_requests, class_name: "Oauth::PendingRequest", foreign_key: :oauth_client_id, dependent: :delete_all,
                                inverse_of: :client

    validates :kind, inclusion: { in: KINDS }
    validates :client_id, presence: true, uniqueness: true
    validates :name, presence: true, length: { maximum: 100 }

    # Whether uri is an allowed redirect URI: https:// with a host, or
    # http:// at a loopback host, with no fragment, at most 512 characters.
    def self.allowed_redirect_uri?(uri)
      return false unless uri.is_a?(String) && uri.length <= MAX_URI

      parsed = URI(uri)
      return false if parsed.host.blank? || parsed.fragment || parsed.userinfo

      case parsed.scheme&.downcase
      when "https" then true
      when "http" then loopback?(parsed.host)
      else false
      end
    rescue URI::InvalidURIError
      false
    end

    def self.loopback?(host)
      Issuer::LOOPBACK_HOSTS.include?(host.to_s.downcase)
    end

    # Whether uri matches one of the registered redirect URIs: exactly, or
    # for loopback, on scheme, path and query with any port, the three
    # loopback hosts being one host (RFC 8252 §7.3).
    def redirect_uri_registered?(uri)
      return false unless uri.is_a?(String) && uri.length <= MAX_URI

      redirect_uris.any? { |registered| registered == uri || same_loopback_uri?(registered, uri) }
    end

    # Whether every redirect URI is loopback, so the client runs on a computer.
    def loopback_only?
      redirect_uris.present? && redirect_uris.all? { |uri| self.class.loopback?(URI(uri).host) }
    rescue URI::InvalidURIError
      false
    end

    # Whether the name is the client's own claim (DCR), which Console did
    # not check.
    def name_checked?
      false
    end

    def public_client?
      token_endpoint_auth_method == "none"
    end

    private

    def same_loopback_uri?(registered, uri)
      a = URI(registered)
      b = URI(uri)
      a.scheme == "http" && b.scheme == "http" && self.class.loopback?(a.host) && self.class.loopback?(b.host) &&
        a.path == b.path && a.query == b.query && b.fragment.nil? && b.userinfo.nil?
    rescue URI::InvalidURIError
      false
    end
  end
end
