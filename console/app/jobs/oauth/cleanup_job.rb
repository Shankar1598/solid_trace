# frozen_string_literal: true

module Oauth
  # Deletes, once a day, the OAuth rows that no one can see or use any more:
  # - tokens that expired more than KEEP_TOKENS ago, and the tokens of
  #   Grants revoked more than KEEP_TOKENS ago (a revoke writes revoked_at
  #   on the Grant, not on its tokens);
  # - Grants that have had no token and are older than GRANT_WITHOUT_TOKEN:
  #   their code expired long ago, so they can never get a token;
  # - expired codes and expired pending requests;
  # - clients older than KEEP_CLIENTS with no Grant, code or pending request,
  #   registered (DCR) or published (CIMD, as a failed first fetch leaves
  #   one). A client in use on an approval page stays.
  class CleanupJob < ApplicationJob
    queue_as :default

    KEEP_TOKENS = 7.days
    GRANT_WITHOUT_TOKEN = 10.minutes
    KEEP_CLIENTS = 7.days

    def perform
      now = Time.current
      Token.where(expires_at: ...now - KEEP_TOKENS).or(Token.where(revoked_at: ...now - KEEP_TOKENS)).delete_all
      Token.where(oauth_grant_id: Grant.where(revoked_at: ...now - KEEP_TOKENS).select(:id)).delete_all
      Grant.where(first_token_at: nil, created_at: ...now - GRANT_WITHOUT_TOKEN).destroy_all
      Code.where(expires_at: ...now).delete_all
      PendingRequest.expired.delete_all
      Client.where(created_at: ...now - KEEP_CLIENTS).where.missing(:grants, :codes, :pending_requests).delete_all
    end
  end
end
