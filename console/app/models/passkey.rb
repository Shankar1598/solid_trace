# frozen_string_literal: true

# A Passkey (GLOSSARY): a credential on the user's device, made for one host,
# the relying-party ID (the Console URL's host when it was made, ADR 0018).
# Console keeps its credential id and public key, never a secret, and the
# authenticator's sign count, which only grows on authenticators that keep
# one, so a cloned credential shows up as a count that went back.
class Passkey < ApplicationRecord
  NAME_LIMIT = 100

  # What the webauthn gem raises reading and checking a browser's answer that
  # is malformed or wrong: its own errors, those of the parsers under it
  # (CBOR, COSE, JSON, base64, truncated binary, OpenSSL), and the
  # NoMethodError, TypeError and KeyError it raises on a field of the wrong
  # type, as it doesn't check the answer's shape. Rescued only around the
  # gem's calls (read_answer), so a bug in Console's own code still raises.
  REFUSED = [ WebAuthn::Error, CBOR::UnpackError, COSE::Error, JSON::ParserError, ArgumentError, IOError,
              OpenSSL::PKey::PKeyError, NoMethodError, TypeError, KeyError ].freeze

  belongs_to :user

  normalizes :name, with: ->(name) { name.strip }
  validates :name, presence: true, length: { maximum: NAME_LIMIT }

  # The Passkey among these, made for offer's host, whose authenticator
  # answered challenge with credential (a PublicKeyCredential's JSON), with
  # user verification and a sign count that went up; nil when none did. It
  # records the new sign count and when it was used.
  def self.accept(credential, challenge:, offer:)
    return unless challenge && (relying_party = offer.relying_party)

    assertion, user_handle = read_answer(credential) do
      answer = WebAuthn::Credential.from_get(credential, relying_party: relying_party)
      [ answer, answer.user_handle ]
    end
    passkey = assertion && where(host: offer.host).find_by(credential_id: assertion.id)
    return unless passkey
    return if user_handle && user_handle != passkey.user.webauthn_id
    return unless read_answer(credential) {
      assertion.verify(challenge, public_key: passkey.public_key, sign_count: passkey.sign_count, user_verification: true)
    }

    passkey.update!(sign_count: assertion.sign_count, last_used_at: Time.current)
    passkey
  end

  # Yields the gem's work on credential, a browser's answer, and returns what
  # the block returns; nil when it isn't a public-key credential or the gem
  # refuses it (REFUSED).
  def self.read_answer(credential)
    return unless credential.is_a?(Hash) && credential["type"] == "public-key"

    yield
  rescue *REFUSED
    nil
  end
end
