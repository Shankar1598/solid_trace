# frozen_string_literal: true

# A user's Two-factor (GLOSSARY): an Authenticator app, one per user, any
# Passkey (User::Passkeys, whatever host it was made for, ADR 0018), or
# both, with Recovery codes. "Has Two-factor" (two_factor?) and "what the
# second step accepts" (pass_second_step) are each answered here alone.
#
# The Authenticator app is TOTP per RFC 6238 (6 digits, 30 s, SHA-1). Its
# secret is encrypted as the SMTP password is (ADR 0015): a changed
# SECRET_KEY_BASE makes it unreadable, and then no code is accepted. Until a
# code from the app confirms it, it is only being set up and isn't
# Two-factor. A code is accepted for its time step and one either side, and
# only for a step later than the last one accepted, so none works twice.
module User::TwoFactor
  extend ActiveSupport::Concern

  ISSUER = "SolidTrace"
  STEP = 30

  # Removing the user's last factor while Organizations of theirs require
  # Two-factor; the message names them.
  class Required < StandardError; end

  included do
    encrypts :authenticator_secret
    has_many :recovery_codes, dependent: :delete_all
  end

  # The Recovery codes gaining_two_factor made, to show the user once.
  attr_reader :new_recovery_codes

  # Whether a password alone no longer signs the user in.
  def two_factor?
    authenticator_app? || passkeys.exists?
  end

  # Checks an answer to the second step (the request's, as
  # UsesPasskeys#second_step_answer gives it): :recovery_code, one of the
  # user's Recovery codes; else :passkey, an answer from one of the user's
  # Passkeys ({credential:, challenge:, offer:}, Passkey.accept); else :code,
  # a code from the Authenticator app. Returns what was accepted
  # (:recovery_code, :passkey or :authenticator_app), or nil. An accepted
  # answer is used up.
  def pass_second_step(answer)
    return unless two_factor?

    if answer[:recovery_code].present?
      :recovery_code if accept_recovery_code(answer[:recovery_code])
    elsif (passkey = answer[:passkey])
      :passkey if passkeys.accept(passkey[:credential], challenge: passkey[:challenge], offer: passkey[:offer])
    elsif authenticator_app? && accept_authenticator_code(answer[:code])
      :authenticator_app
    end
  end

  # Yields the change that may give the user Two-factor, and when it does
  # (they had none before) ends their other Sessions, keeping keep, and makes
  # a set of Recovery codes, left in new_recovery_codes to show once. Returns
  # what the block returns.
  def gaining_two_factor(keep:)
    had = two_factor?
    @new_recovery_codes = nil
    yield.tap do |changed|
      if changed && !had && two_factor?
        sessions.where.not(id: keep).destroy_all
        @new_recovery_codes = make_recovery_codes
      end
    end
  end

  # Replaces the user's Recovery codes with a new set. Returns the codes,
  # which Console doesn't keep.
  def make_recovery_codes
    codes = Array.new(RecoveryCode::SET_SIZE) { RecoveryCode.generate }
    transaction do
      recovery_codes.delete_all
      recovery_codes.insert_all!(codes.map { |code| { digest: RecoveryCode.digest(code) } })
    end
    codes
  end

  def recovery_codes_left
    recovery_codes.unused.count
  end

  def authenticator_app?
    authenticator_confirmed_at.present?
  end

  # Starts setting up an Authenticator app with a new secret, or keeps the
  # one already being set up, so a reloaded page shows the same QR code.
  def set_up_authenticator_app
    return if authenticator_app?
    return if authenticator_secret_readable?

    update!(authenticator_secret: ROTP::Base32.random, authenticator_last_step: nil)
  end

  # The otpauth:// URI the QR code holds: issuer SolidTrace, the user's email.
  def authenticator_uri
    authenticator_totp.provisioning_uri(email)
  end

  # Confirms the app being set up with a code from it. Returns whether it did.
  def confirm_authenticator_app(code)
    return false if authenticator_app? || !authenticator_secret_readable?
    return false unless accept_authenticator_code(code)

    update!(authenticator_confirmed_at: Time.current)
  end

  # Removes the Authenticator app, for a right second-step answer
  # (remove_factor).
  def remove_authenticator_app(answer)
    remove_factor(answer) { forget_authenticator_app }
  end

  # Removes passkey, one of the user's, made for any host, for a right
  # second-step answer (remove_factor), which may come from that Passkey.
  def remove_passkey(passkey, answer)
    remove_factor(answer) { passkey.destroy! }
  end

  # The operator's reset (users:reset_two_factor), for a user who has lost
  # every factor and every Recovery code: removes them all (the Authenticator
  # app, every Passkey, the Recovery codes) and ends the user's Sessions, so
  # they next sign in with their password alone. Require two-factor doesn't
  # stop it; the user is asked to set it up again.
  def reset_two_factor
    transaction do
      forget_authenticator_app
      passkeys.delete_all
      recovery_codes.delete_all
      sessions.destroy_all
    end
  end

  # The Organizations of the user's that have Require two-factor, by name.
  def organizations_requiring_two_factor
    organizations.where(require_two_factor: true).order(:name)
  end

  private

  # Removes one of the user's factors, the block's change, for a right
  # second-step answer given in the same request: the one place a user
  # removes a factor. Returns whether it did, false for a wrong answer.
  # Removing the last factor throws the Recovery codes away, and is refused
  # while any of the user's Organizations requires Two-factor: then nothing
  # changes, the answer isn't used up, and it raises Required naming them.
  def remove_factor(answer)
    transaction do
      next false unless pass_second_step(answer)

      yield
      unless two_factor?
        requiring = organizations_requiring_two_factor.pluck(:name)
        raise Required, "#{requiring.to_sentence} #{requiring.one? ? 'requires' : 'require'} Two-factor, " \
                        "so you can't remove your last factor." if requiring.any?

        recovery_codes.delete_all
      end
      true
    end
  rescue Required
    reload
    raise
  end

  def forget_authenticator_app
    update!(authenticator_secret: nil, authenticator_confirmed_at: nil, authenticator_last_step: nil)
  end

  # Marks the Recovery code used, only if it is unused, in one UPDATE, so two
  # requests with one code can't both pass.
  def accept_recovery_code(code)
    recovery_codes.unused.where(digest: RecoveryCode.digest(code)).update_all(used_at: Time.current) == 1
  end

  def authenticator_totp
    ROTP::TOTP.new(authenticator_secret, issuer: ISSUER, interval: STEP)
  end

  def authenticator_secret_readable?
    authenticator_secret.present?
  rescue ActiveRecord::Encryption::Errors::Decryption
    false
  end

  # Records the code's step as the last accepted one, only if it is later
  # than the last, in one UPDATE, so two requests with one code can't both
  # pass.
  def accept_authenticator_code(code)
    code = code.to_s.gsub(/\s/, "")
    return false unless code.match?(/\A\d{6}\z/) && authenticator_secret_readable?

    after = authenticator_last_step && authenticator_last_step * STEP
    at = authenticator_totp.verify(code, drift_behind: STEP, drift_ahead: STEP, after: after)
    return false unless at

    step = at / STEP
    accepted = User.where(id: id)
      .where("authenticator_last_step IS NULL OR authenticator_last_step < ?", step)
      .update_all(authenticator_last_step: step) == 1
    self.authenticator_last_step = step if accepted
    accepted
  end
end
