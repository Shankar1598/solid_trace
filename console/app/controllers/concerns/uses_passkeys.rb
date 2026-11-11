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
  # One challenge per purpose, as adding a Passkey can be proven with another.
  def remember_passkey_challenge(purpose, options)
    session[:passkey_challenges] = kept_passkey_challenges.merge(
      purpose.to_s => { "challenge" => options.challenge, "at" => Time.current.to_i },
    )
    options
  end

  # The challenge kept for purpose, used up; nil when there's none or it's
  # too old.
  def take_passkey_challenge(purpose)
    kept = kept_passkey_challenges[purpose.to_s]
    session[:passkey_challenges] = kept_passkey_challenges.except(purpose.to_s)
    return unless kept.is_a?(Hash) && Time.at(kept["at"].to_i) > CHALLENGE_LASTS.ago

    kept["challenge"]
  end

  def kept_passkey_challenges
    kept = session[:passkey_challenges]
    kept.is_a?(Hash) ? kept : {}
  end

  # The browser's PublicKeyCredential, as its toJSON() gives it.
  def passkey_credential(credential = params[:credential])
    credential.respond_to?(:to_unsafe_h) ? credential.to_unsafe_h : {}
  end

  # The request's answer to the second step, for User#pass_second_step, from
  # given (the request's params, or a part of them): a code, a Recovery code,
  # or an answer from a Passkey to the challenge kept for purpose, which only
  # a page opened at the Console URL can give.
  def second_step_answer(purpose, given = params)
    answer = { code: given[:code], recovery_code: given[:recovery_code] }
    if given[:credential].present? && passkeys_here?
      answer[:passkey] = { credential: passkey_credential(given[:credential]), challenge: take_passkey_challenge(purpose), offer: passkey_offer }
    end
    answer
  end

  # What a wrong second-step answer is told, by the kind it was.
  def second_step_refused(given = params)
    if given[:credential].present?
      "That Passkey didn't work. Try again."
    else
      "That code didn't work. Try the newest one from your app."
    end
  end

  # The request's proof for adding a factor (User#add_factor), under proof:
  # the password, or a second-step answer, a Passkey's to the proof options.
  def factor_proof
    second_step_answer(:proof, proof_params).merge(password: proof_params[:password])
  end

  # What wrong proof for adding a factor is told.
  def factor_proof_refused
    current_user.two_factor? ? second_step_refused(proof_params) : "That password didn't work."
  end

  def proof_params
    proof = params[:proof]
    proof.is_a?(ActionController::Parameters) ? proof : ActionController::Parameters.new
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
