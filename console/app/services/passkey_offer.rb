# frozen_string_literal: true

require "ipaddr"

# Where Passkeys are offered (ADR 0018): one question, answered from the fixed
# Console URL, the Console URL setting else CONSOLE_URL (PublicUrls), never the
# address a request arrived on, so a Passkey is always made for the host
# people reach Console at. They're offered at an https:// URL on a domain, or
# at http://localhost for development and tests; anywhere else, not, with the
# reason. There's no switch: an install either can have them or can't.
#
# The URL's host is the relying-party ID every Passkey is made for, and only
# a page opened at the URL itself can use them (here?).
class PasskeyOffer
  NAME = "SolidTrace"

  attr_reader :url, :host, :reason

  def self.current(settings: InstanceSetting.current, env: ENV)
    new(PublicUrls.console_url(nil, settings: settings, env: env))
  end

  # What changing the instance settings from saved to changed (an unsaved
  # InstanceSetting) strands, as stranded_by says; nil when nothing.
  def self.stranded_by_change(saved:, changed:)
    current(settings: saved).stranded_by(current(settings: changed))
  end

  def initialize(url)
    @url = url
    @reason = refusal
    @host = URI(url).host if offered?
  end

  def offered?
    reason.nil?
  end

  # Whether a page opened at request_url (scheme, host and port) can use
  # Passkeys: only at the Console URL itself, as the browser checks the page's
  # origin against the host they're made for.
  def here?(request_url)
    offered? && origin(request_url) == origin(url)
  end

  # The webauthn gem's relying party for this host and origin; nil when
  # Passkeys aren't offered.
  def relying_party
    return unless offered?

    WebAuthn::RelyingParty.new(id: host, name: NAME, allowed_origins: [ origin(url) ])
  end

  # What moving to other (another offer) strands: the Passkeys made for this
  # host, when other's host differs or other offers none, as a sentence with
  # their count and their users'; nil when it strands none.
  def stranded_by(other)
    return if !offered? || other.host == host

    passkeys = Passkey.where(host: host)
    count = passkeys.count
    return if count.zero?

    users = passkeys.distinct.count(:user_id)
    "#{count} #{"Passkey".pluralize(count)} of #{users} #{"user".pluralize(users)} would stop working: " \
      "they're made for #{host}, the Console URL's host now, and work only there."
  end

  # Settings → Instance's and get_instance_settings' row: { offered: true,
  # host:, url: } or { offered: false, reason: }.
  def as_json(*)
    offered? ? { offered: true, host: host, url: url } : { offered: false, reason: reason }
  end

  private

  def refusal
    return "Console has no fixed Console URL. Set the Console URL, or CONSOLE_URL in deploy/.env." if url.blank?

    uri = URI(url)
    return "The Console URL, #{url}, is an IP address, not a domain." if ip_address?(uri.hostname)
    return if uri.scheme == "https" || (uri.scheme == "http" && uri.host == "localhost")

    "The Console URL, #{url}, isn't HTTPS."
  rescue URI::InvalidURIError
    "The Console URL, #{url}, isn't a URL."
  end

  def ip_address?(host)
    IPAddr.new(host.to_s)
    true
  rescue IPAddr::InvalidAddressError
    false
  end

  def origin(url)
    uri = URI(url.to_s)
    "#{uri.scheme}://#{uri.host}#{":#{uri.port}" unless uri.port == uri.default_port}"
  rescue URI::InvalidURIError
    nil
  end
end
