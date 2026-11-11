# frozen_string_literal: true

# The user's Passkeys on Settings → User: adding one (the browser's options,
# then the Passkey it made), renaming, and removing one, which needs a
# second-step answer in the same request (a code, a Recovery code, or an
# answer from one of the user's Passkeys to the proof options). Only a page
# opened at the Console URL can add Passkeys or answer with one
# (UsesPasskeys). Adding and removing are rate-limited as signing in is.
class PasskeysController < ApplicationController
  include UsesPasskeys
  include ShowsRecoveryCodes

  rate_limit to: 10, within: 3.minutes, only: [ :create, :destroy ],
    with: -> { redirect_back_or_to user_settings_path, alert: "Too many tries. Try again in a few minutes." }
  before_action :refuse_passkey_options_elsewhere, only: [ :creation_options, :proof_options ]
  before_action :set_passkey, only: [ :update, :destroy ]

  def creation_options
    render json: remember_passkey_challenge(:add, current_user.passkey_creation_options(passkey_offer))
  end

  # A first Passkey, as the user's first factor, gives them Two-factor:
  # their other Sessions end and their Recovery codes are shown once.
  def create
    passkey = passkeys_here? && current_user.gaining_two_factor(keep: Current.session) {
      current_user.add_passkey(passkey_credential, challenge: take_passkey_challenge(:add), offer: passkey_offer, name: params[:name])
    }
    if passkey && current_user.new_recovery_codes
      redirect_showing_recovery_codes current_user.new_recovery_codes, continue_to: user_settings_path, notice: "Passkey added."
    elsif passkey
      redirect_back_or_to user_settings_path, notice: "Passkey added."
    else
      redirect_back_or_to user_settings_path, alert: passkeys_here? ? "That passkey couldn't be added. Try again." : passkeys_elsewhere
    end
  end

  def update
    if @passkey.update(name: params[:name].to_s)
      redirect_back_or_to user_settings_path, notice: "Passkey renamed."
    else
      redirect_back_or_to user_settings_path, alert: "Name #{@passkey.errors[:name].to_sentence}."
    end
  end

  # Options asking for one of the user's Passkeys here, to prove it's them
  # when removing a factor or making new Recovery codes.
  def proof_options
    render json: passkey_proof_options(current_user, :proof)
  end

  # Any of the user's Passkeys can be removed, those made for another host
  # too, for a second-step answer.
  def destroy
    if current_user.remove_passkey(@passkey, second_step_answer(:proof))
      notice = current_user.two_factor? ? "Passkey removed." : "Passkey removed. Two-factor is off."
      redirect_back_or_to user_settings_path, notice: notice
    else
      redirect_back_or_to user_settings_path, alert: second_step_refused
    end
  rescue User::TwoFactor::Required => e
    redirect_back_or_to user_settings_path, alert: e.message
  end

  private

  def set_passkey
    @passkey = current_user.passkeys.find(params[:id])
  end
end
