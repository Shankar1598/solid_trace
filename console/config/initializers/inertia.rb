# frozen_string_literal: true

InertiaRails.configure do |config|
  # Use ViteRuby digest for cache busting. ViteRuby.digest reads the files in
  # a Dir.chdir block, and Ruby raises when another thread changes the
  # directory at the same time. Puma answers requests in several threads, so
  # one thread at a time reads the digest.
  digest_lock = Mutex.new
  config.version = -> { digest_lock.synchronize { ViteRuby.digest } }

  # Default root view
  config.default_render = true
  config.always_include_errors_hash = true
end
