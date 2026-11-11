# frozen_string_literal: true

# A Passkey (GLOSSARY): a credential on the user's device, made for one host,
# the relying-party ID (the Console URL's host when it was made, ADR 0018).
# Console keeps its credential id and public key, never a secret, and the
# authenticator's sign count, which only grows on authenticators that keep
# one, so a cloned credential shows up as a count that went back.
class Passkey < ApplicationRecord
  NAME_LIMIT = 100

  belongs_to :user

  normalizes :name, with: ->(name) { name.strip }
  validates :name, presence: true, length: { maximum: NAME_LIMIT }

  # The Passkey among these, made for offer's host, whose authenticator
  # answered challenge with credential (a PublicKeyCredential's JSON), with
  # user verification and a sign count that went up; nil when none did. It
  # records the new sign count and when it was used.
  def self.accept(credential, challenge:, offer:)
    return unless challenge && (relying_party = offer.relying_party)

    assertion = WebAuthn::Credential.from_get(credential, relying_party: relying_party)
    passkey = where(host: offer.host).find_by(credential_id: assertion.id)
    return unless passkey
    return if assertion.user_handle && assertion.user_handle != passkey.user.webauthn_id
    return unless verified?(assertion, challenge, passkey)

    passkey.update!(sign_count: assertion.sign_count, last_used_at: Time.current)
    passkey
  rescue *User::Passkeys::REFUSED
    nil
  end

  def self.verified?(assertion, challenge, passkey)
    assertion.verify(challenge, public_key: passkey.public_key, sign_count: passkey.sign_count, user_verification: true)
  end
  private_class_method :verified?
end
