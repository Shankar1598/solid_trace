# frozen_string_literal: true

# Active Record Encryption's keys, derived from SECRET_KEY_BASE, so encrypted
# columns (the SMTP password) need no other secret (ADR 0015). A changed
# SECRET_KEY_BASE makes them unreadable. Under SECRET_KEY_BASE_DUMMY, as when
# the image precompiles its assets, Rails makes up a secret, which is enough.
Rails.application.config.active_record.encryption.tap do |encryption|
  key_generator = Rails.application.key_generator
  encryption.primary_key = key_generator.generate_key("active_record_encryption/primary_key", 32).unpack1("H*")
  encryption.key_derivation_salt = key_generator.generate_key("active_record_encryption/key_derivation_salt", 32).unpack1("H*")
end
