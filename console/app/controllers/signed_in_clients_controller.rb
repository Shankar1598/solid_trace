# frozen_string_literal: true

# Revokes one of the user's Signed-in clients (a Grant) from Settings →
# User. Its OAuth tokens stop at once: each use checks that the Grant is
# live. Only the user's own Grants that are not revoked yet are found, so
# another user's Grant is 404.
class SignedInClientsController < ApplicationController
  def destroy
    current_user.oauth_grants.where(revoked_at: nil).find(params[:id]).revoke!
    redirect_back_or_to user_settings_path, notice: "Signed-in client revoked"
  end
end
