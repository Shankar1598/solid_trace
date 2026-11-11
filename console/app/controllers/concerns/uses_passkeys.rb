# frozen_string_literal: true

# What the pages that use Passkeys share: where they're offered
# (PasskeyOffer), whether this page was opened there, and the challenge each
# ceremony signs, kept in the Rails session for CHALLENGE_LASTS and used
# once, for the purpose it was made for.
module UsesPasskeys
  extend ActiveSupport::Concern

  CHALLENGE_LASTS = 5.minutes

  private

  def passkey_offer
    @passkey_offer ||= PasskeyOffer.current
  end

  # Whether this page can use Passkeys: only at the Console URL itself.
  def passkeys_here?
    passkey_offer.here?(request.base_url)
  end

  # For a page: where Passkeys are offered, and whether here.
  def passkey_offer_prop
    passkey_offer.as_json.merge(here: passkeys_here?)
  end

  # What a page opened elsewhere says instead of the Passkey controls.
  def passkeys_elsewhere
    if passkey_offer.offered?
      "Passkeys work at #{passkey_offer.url}."
    else
      "Passkeys aren't offered on this install. #{passkey_offer.reason}"
    end
  end

  # Keeps options' challenge for purpose and returns options, for the browser.
  def remember_passkey_challenge(purpose, options)
    session[:passkey_challenge] = { "purpose" => purpose.to_s, "challenge" => options.challenge, "at" => Time.current.to_i }
    options
  end

  # The challenge kept for purpose, used up; nil when there's none, it was for
  # something else, or it's too old.
  def take_passkey_challenge(purpose)
    kept = session.delete(:passkey_challenge)
    return unless kept.is_a?(Hash) && kept["purpose"] == purpose.to_s
    return unless Time.at(kept["at"].to_i) > CHALLENGE_LASTS.ago

    kept["challenge"]
  end

  # The browser's PublicKeyCredential, as its toJSON() gives it.
  def passkey_credential
    credential = params[:credential]
    credential.respond_to?(:to_unsafe_h) ? credential.to_unsafe_h : {}
  end

  # The options endpoints answer JSON; a page opened elsewhere gets why.
  def refuse_passkey_options_elsewhere
    render json: { error: passkeys_elsewhere }, status: :unprocessable_entity unless passkeys_here?
  end
end
