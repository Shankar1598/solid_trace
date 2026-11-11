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

  # The request's answer to the second step, for User#pass_second_step: a
  # code, a Recovery code, or an answer from a Passkey to the challenge kept
  # for purpose, which only a page opened at the Console URL can give.
  def second_step_answer(purpose)
    answer = { code: params[:code], recovery_code: params[:recovery_code] }
    if params[:credential].present? && passkeys_here?
      answer[:passkey] = { credential: passkey_credential, challenge: take_passkey_challenge(purpose), offer: passkey_offer }
    end
    answer
  end

  # What a wrong second-step answer is told, by the kind it was.
  def second_step_refused
    if params[:credential].present?
      "That passkey didn't work. Try again."
    else
      "That code didn't work. Try the newest one from your app."
    end
  end

  # Options asking for an answer from one of user's Passkeys for this host,
  # with user verification, its challenge kept for purpose.
  def passkey_proof_options(user, purpose)
    options = passkey_offer.relying_party.options_for_authentication(
      allow: user.passkeys.where(host: passkey_offer.host).pluck(:credential_id), user_verification: "required",
    )
    remember_passkey_challenge(purpose, options)
  end

  # The options endpoints answer JSON; a page opened elsewhere gets why.
  def refuse_passkey_options_elsewhere
    render json: { error: passkeys_elsewhere }, status: :unprocessable_entity unless passkeys_here?
  end
end
