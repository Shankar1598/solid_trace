# frozen_string_literal: true

# A user's Passkeys (ADR 0018), made for the host PasskeyOffer gives. Each
# is a discoverable credential with user verification, so it signs the user
# in alone, from the sign-in page, with no password and no email typed.
# The user's WebAuthn user handle, webauthn_id, is made with their first.
#
# A Passkey is a factor of the user's Two-factor: counting it, accepting one
# at the second step and removing one are User::TwoFactor's.
module User::Passkeys
  extend ActiveSupport::Concern

  included do
    has_many :passkeys, dependent: :delete_all
  end

  # The browser's options for adding a Passkey for offer's host: a
  # discoverable credential with user verification, on any authenticator,
  # but not one already holding a Passkey of the user's for this host.
  def passkey_creation_options(offer)
    update!(webauthn_id: WebAuthn.generate_user_id) if webauthn_id.blank?
    offer.relying_party.options_for_registration(
      user: { id: webauthn_id, name: email, display_name: name },
      exclude: passkeys.where(host: offer.host).pluck(:credential_id),
      authenticator_selection: { residentKey: "required", requireResidentKey: true, userVerification: "required" },
    )
  end

  # Adds the Passkey the browser made, credential, for challenge and offer's
  # host, named name. Returns it, or nil when the browser's answer doesn't
  # check out.
  def add_passkey(credential, challenge:, offer:, name:)
    return unless challenge && (relying_party = offer.relying_party)

    made = Passkey.read_answer(credential) { relying_party.verify_registration(credential, challenge, user_verification: true) }
    return unless made

    passkeys.create!(credential_id: made.id, public_key: made.public_key, sign_count: made.sign_count,
      host: offer.host, name: name.to_s.strip.presence || "Passkey")
  end
end
