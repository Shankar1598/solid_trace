# frozen_string_literal: true

module Oauth
  # Clears the new pair's secrets that a used refresh token keeps for its
  # 60-second window (Token#rotate!). It runs after the window; a use after
  # the window never reads them, also when this job runs late.
  class ClearKeptPairJob < ApplicationJob
    queue_as :default

    def perform(token_id)
      Token.where(id: token_id).update_all(kept_access_token: nil, kept_refresh_token: nil)
    end
  end
end
