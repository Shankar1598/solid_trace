require 'digest'
require 'securerandom'
require 'benchmark/ips' # Note the different require

uuid_input = SecureRandom.uuid

Benchmark.ips do |x|
  # Configure the benchmark (optional)
  x.config(time: 5, warmup: 2)

  x.report("MD5") { Digest::MD5.hexdigest(uuid_input) }
  x.report("SHA1") { Digest::SHA1.hexdigest(uuid_input) }
  x.report("SHA256") { Digest::SHA256.hexdigest(uuid_input) }
  x.report("SHA512") { Digest::SHA512.hexdigest(uuid_input) }

  # This is the magic line that DOES NOT exist in standard Benchmark
  x.compare!
end
