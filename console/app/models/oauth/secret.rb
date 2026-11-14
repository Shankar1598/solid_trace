# frozen_string_literal: true

module Oauth
  # The random secrets OAuth gives out (codes, OAuth tokens, pending-request
  # keys) and the SHA-256 digests Console keeps of them in their place.
  module Secret
    BYTES = 32

    module_function

    # prefix + 32 random bytes, base64url with no padding.
    def generate(prefix = "")
      prefix + SecureRandom.urlsafe_base64(BYTES)
    end

    def digest(secret)
      Digest::SHA256.hexdigest(secret)
    end

    # The PKCE S256 challenge for a code_verifier (RFC 7636 §4.2).
    def s256(verifier)
      Base64.urlsafe_encode64(Digest::SHA256.digest(verifier), padding: false)
    end
  end
end
