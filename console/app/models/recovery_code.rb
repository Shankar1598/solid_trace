# frozen_string_literal: true

# One of a user's Recovery codes (GLOSSARY), kept as a SHA-256 digest. A code
# is 16 random characters (80 bits), so its digest needs no key or slow hash:
# it can't be guessed from the digest, and it survives a changed
# SECRET_KEY_BASE, when Recovery codes are how users get back in.
class RecoveryCode < ApplicationRecord
  SET_SIZE = 10
  # Digits and lowercase letters without i, l, o and u, which are misread.
  ALPHABET = "0123456789abcdefghjkmnpqrstvwxyz".chars.freeze

  belongs_to :user

  scope :unused, -> { where(used_at: nil) }

  # A new code, as the user sees it: four groups of four.
  def self.generate
    Array.new(16) { ALPHABET.sample(random: SecureRandom) }.join.scan(/.{4}/).join("-")
  end

  # The digest of a code as typed: case, spaces and dashes don't matter.
  def self.digest(code)
    OpenSSL::Digest::SHA256.hexdigest(code.to_s.downcase.gsub(/[^0-9a-z]/, ""))
  end
end
