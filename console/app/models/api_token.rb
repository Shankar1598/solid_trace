# frozen_string_literal: true

# A secret a user makes in their settings that lets a client act as that user
# through the MCP server. The secret is shown once when the token is made;
# Console keeps only its SHA-256 digest and finds the token by it.
class ApiToken < ApplicationRecord
  PREFIX = "st_"
  SECRET_BYTES = 32
  # last_used_at is written at most this often, so a busy client doesn't take
  # SQLite's write lock on every call.
  TOUCH_EVERY = 1.minute

  belongs_to :user

  validates :name, presence: true, length: { maximum: 100 }
  validates :token_digest, presence: true, uniqueness: true

  # Makes a token for user and returns it with its secret, which is never
  # stored. Raises ActiveRecord::RecordInvalid when the name is blank.
  def self.issue!(user:, name:)
    secret = PREFIX + SecureRandom.urlsafe_base64(SECRET_BYTES)
    token = create!(user: user, name: name.to_s.strip, token_digest: digest(secret))
    [ token, secret ]
  end

  # The token whose secret this is, or nil.
  def self.find_by_secret(secret)
    return nil unless secret.is_a?(String) && secret.start_with?(PREFIX)

    find_by(token_digest: digest(secret))
  end

  def self.digest(secret)
    Digest::SHA256.hexdigest(secret)
  end

  # Records a use, unless one was recorded in the last minute.
  def touch_last_used
    now = Time.current
    return if last_used_at && last_used_at > now - TOUCH_EVERY

    update_column(:last_used_at, now)
  end
end
