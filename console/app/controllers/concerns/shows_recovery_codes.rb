# frozen_string_literal: true

# Recovery codes are shown once, on the page RecoveryCodesController#show
# draws from the flash: the codes and where Continue goes, in the encrypted
# session cookie for the one request after this redirect.
module ShowsRecoveryCodes
  extend ActiveSupport::Concern

  private

  # Redirects to continue_to, by way of the Recovery codes page when codes is
  # a new set (User#new_recovery_codes, once gaining_two_factor gave
  # Two-factor).
  def redirect_showing_recovery_codes(codes, continue_to:, **flash_messages)
    return redirect_to(continue_to, flash_messages) unless codes

    flash[:recovery_codes] = { "codes" => codes, "continue_to" => continue_to }
    redirect_to recovery_codes_path, flash_messages
  end
end
