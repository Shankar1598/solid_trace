# frozen_string_literal: true

module Oauth
  # A request to the approval page whose client and redirect URI Console
  # checked, kept under a random key for 10 minutes; each new request
  # deletes the expired ones (.keep!). The browser's Rails session holds
  # only the keys of its own requests, at most 3 (#remember), so the cookie
  # stays small and another browser cannot use a key.
  class PendingRequest < ApplicationRecord
    LASTS = 10.minutes
    # How many pending requests one browser keeps.
    KEEP = 3
    SESSION_KEY = :oauth_requests

    belongs_to :client, class_name: "Oauth::Client", foreign_key: :oauth_client_id, inverse_of: :pending_requests

    before_validation -> { self.key ||= Secret.generate }, on: :create

    scope :fresh, -> { where(created_at: LASTS.ago..) }
    scope :expired, -> { where(created_at: ...LASTS.ago) }

    # Keeps a new request, and deletes the expired ones, so the table holds
    # only the requests of the last 10 minutes.
    def self.keep!(**attributes)
      expired.delete_all
      create!(**attributes)
    end

    # The fresh request with key, if session holds key; else nil.
    def self.find_in_session(session, key)
      return unless key.is_a?(String) && Array(session[SESSION_KEY]).include?(key)

      fresh.find_by(key: key)
    end

    # Adds the request's key to session, dropping the oldest key, and its
    # row, past KEEP.
    def remember(session)
      keys = Array(session[SESSION_KEY]) + [ key ]
      dropped = keys.shift([ keys.size - KEEP, 0 ].max)
      self.class.where(key: dropped).delete_all if dropped.any?
      session[SESSION_KEY] = keys
    end

    # Deletes the request and its key from session.
    def finish(session)
      session[SESSION_KEY] = Array(session[SESSION_KEY]) - [ key ]
      destroy
    end
  end
end
