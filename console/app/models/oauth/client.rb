# frozen_string_literal: true

module Oauth
  # An OAuth client. A registered (DCR) client is public (it sends its
  # client_id) or confidential (it also sends its client secret, which
  # Console keeps as a SHA-256 digest); both prove the code with PKCE. Its
  # name is the one it gave, which Console cannot check. A client with a
  # published description (cimd) has the description's HTTPS URL as its
  # client_id; the row keeps the last fetched copy (PublishedDescription),
  # and its name counts as checked, because the client owns the host that
  # publishes it.
  class Client < ApplicationRecord
    KINDS = %w[dcr cimd].freeze
    AUTH_METHODS = %w[none client_secret_basic client_secret_post].freeze
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

    # Whether Console checked the name: it comes from a published
    # description. A registered (DCR) name is the client's own claim.
    def name_checked?
      kind == "cimd"
    end

    # The host that publishes the description, for a cimd client.
    def published_host
      URI(client_id).host if kind == "cimd"
    end

    # Where the client is, for Settings → User: the host that publishes the
    # description, or for a registered client the hosts of its redirect
    # URIs.
    def host
      published_host || redirect_uris.filter_map { |uri| uri_host(uri) }.uniq.join(", ")
    end

    # Whether the kept copy of the description is fresh, so no fetch is
    # needed.
    def fresh?
      fetched? && fresh_until&.future?
    end

    def fetched?
      fetched_at.present?
    end

    # Starts a fetch, unless one runs now: a conditional update, so of two
    # requests at once only one starts. A start older than
    # PublishedDescription::FETCH_LASTS no longer counts.
    def start_fetch!
      now = Time.current
      started = self.class.where(id: id)
        .where("fetch_started_at IS NULL OR fetch_started_at < ?", now - PublishedDescription::FETCH_LASTS)
        .update_all(fetch_started_at: now)
      started == 1
    end

    def end_fetch!
      self.class.where(id: id).update_all(fetch_started_at: nil)
    end

    # Keeps a checked description as this client's copy, fresh for
    # fresh_for. Only its allowed redirect URIs are kept.
    def keep_description!(document, fresh_for:)
      name = document["client_name"]
      name = name.is_a?(String) && name.strip.present? ? name.strip.first(100) : UNNAMED
      grant_types = Array(document["grant_types"]).select { |type| type.is_a?(String) }.presence || DEFAULT_GRANT_TYPES
      now = Time.current
      update!(name: name, redirect_uris: document["redirect_uris"].select { |uri| self.class.allowed_redirect_uri?(uri) },
              grant_types: grant_types, token_endpoint_auth_method: "none", metadata: document,
              fetched_at: now, fresh_until: now + fresh_for)
    end

    def public_client?
      token_endpoint_auth_method == "none"
    end

    # Whether secret authenticates the client: none for a public client;
    # for a confidential client, the secret whose digest Console keeps.
    def authenticates?(secret)
      return secret.nil? if public_client?

      secret.is_a?(String) && secret.present? && client_secret_digest.present? &&
        ActiveSupport::SecurityUtils.secure_compare(Secret.digest(secret), client_secret_digest)
    end

    private

    def uri_host(uri)
      URI(uri).host
    rescue URI::InvalidURIError
      nil
    end

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
