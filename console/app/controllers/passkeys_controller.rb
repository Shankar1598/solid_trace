# frozen_string_literal: true

# The user's Passkeys on Settings → User: adding one (the browser's options,
# then the Passkey it made), renaming, and removing one, which needs an
# answer from that Passkey in the same request. Only a page opened at the
# Console URL can add or remove them (UsesPasskeys). Adding and removing are
# rate-limited as signing in is.
class PasskeysController < ApplicationController
  include UsesPasskeys

  rate_limit to: 10, within: 3.minutes, only: [ :create, :destroy ],
    with: -> { redirect_back_or_to user_settings_path, alert: "Too many tries. Try again in a few minutes." }
  before_action :refuse_passkey_options_elsewhere, only: [ :creation_options, :removal_options ]
  before_action :set_passkey, only: [ :update, :removal_options, :destroy ]

  def creation_options
    render json: remember_passkey_challenge(:add, current_user.passkey_creation_options(passkey_offer))
  end

  def create
    passkey = passkeys_here? && current_user.add_passkey(passkey_credential,
      challenge: take_passkey_challenge(:add), offer: passkey_offer, name: params[:name])
    if passkey
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

  # Options asking for this Passkey alone, to prove removing it.
  def removal_options
    options = passkey_offer.relying_party.options_for_authentication(allow: [ @passkey.credential_id ], user_verification: "required")
    render json: remember_passkey_challenge(removal(@passkey), options)
  end

  def destroy
    if passkeys_here? && current_user.remove_passkey(@passkey, passkey_credential,
        challenge: take_passkey_challenge(removal(@passkey)), offer: passkey_offer)
      redirect_back_or_to user_settings_path, notice: "Passkey removed."
    else
      redirect_back_or_to user_settings_path, alert: passkeys_here? ? "Use the passkey you're removing to remove it." : passkeys_elsewhere
    end
  end

  private

  def set_passkey
    @passkey = current_user.passkeys.find(params[:id])
  end

  def removal(passkey)
    "remove #{passkey.id}"
  end
end
