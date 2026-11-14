# frozen_string_literal: true

module Oauth
  # Device codes (RFC 8628), for clients with a published description only.
  # A device code is a Code of kind device: 32 random bytes, base64url, that
  # the program polls the token endpoint with, kept as a SHA-256 digest. Its
  # user code is 8 letters from LETTERS, which the user types on the device
  # page; Console shows it as XXXX-XXXX and keeps only its digest. No two
  # live device codes have the same user code. A device code lasts 10
  # minutes, and keeps the IP address that asked for it.
  module DeviceCode
    GRANT_TYPE = "urn:ietf:params:oauth:grant-type:device_code"
    LASTS = 10.minutes
    # The seconds that a program waits between two polls, and what slow_down
    # adds to them.
    INTERVAL = 5
    SLOW_DOWN = 5
    # No vowels, so a user code spells no word.
    LETTERS = "BCDFGHJKLMNPQRSTVWXZ"
    LENGTH = 8
    USER_CODE = /\A[#{LETTERS}]{#{LENGTH}}\z/

    module_function

    # Makes a device code for the client at client_id (a published
    # description's URL), and returns it with its secret and its user code
    # as the program shows it.
    def issue!(client_id:, scopes:, resource:, ip_address:)
      secret = Secret.generate
      user_code = new_user_code
      code = Code.create!(kind: "device", code_digest: Secret.digest(secret), client_id: client_id,
                          scopes: scopes.map(&:name), resource: resource, ip_address: ip_address,
                          poll_interval: INTERVAL, user_code_digest: Secret.digest(user_code), expires_at: LASTS.from_now)
      [ code, secret, display(user_code) ]
    end

    # The device code that waits for a decision and whose user code the
    # user typed, in any letter case, with or without the dash; else nil.
    def find_waiting(typed)
      user_code = typed.to_s.upcase.gsub(/[\s-]/, "")
      return unless USER_CODE.match?(user_code)

      live.find_by(user_code_digest: Secret.digest(user_code), oauth_grant_id: nil, denied_at: nil, used_at: nil)
    end

    # The device codes that have not expired.
    def live
      Code.where(kind: "device").where("expires_at > ?", Time.current)
    end

    def new_user_code
      loop do
        user_code = Array.new(LENGTH) { random_letter }.join
        return user_code unless live.exists?(user_code_digest: Secret.digest(user_code))
      end
    end

    def random_letter
      LETTERS[SecureRandom.random_number(LETTERS.length)]
    end

    def display(user_code)
      user_code.scan(/.{4}/).join("-")
    end
  end
end
