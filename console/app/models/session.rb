# frozen_string_literal: true

# A browser signed in as a user, until its expiry: 24 hours from signing in,
# or 7 days with Remember me. Using Console never moves the expiry.
class Session < ApplicationRecord
  LIFETIME = 24.hours
  REMEMBERED_LIFETIME = 7.days

  belongs_to :user

  def self.lifetime(remember_me:)
    remember_me ? REMEMBERED_LIFETIME : LIFETIME
  end

  def expired?
    expires_at <= Time.current
  end
end
