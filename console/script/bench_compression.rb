require 'benchmark/ips'
require 'lz4-ruby'   # gem install lz4-ruby
require 'zstd-ruby'  # gem install zstd-ruby
require 'zlib'       # Standard Ruby Library
require 'securerandom'
require 'json'
require 'date'

# --- 1. Realistic Data Generator ---
# Using the "Pure Ruby" generator to ensure consistent, realistic entropy
puts "Generating realistic sample data..."

DICTIONARY = %w[
  error processed failed success user login logout timeout database
  connection retry invalid forbidden missing parameter payload
  update create delete fetch render view click hover scroll
  system kernel memory disk cpu network latency bandwidth
]

USER_AGENTS = [
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)",
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
  "PostmanRuntime/7.26.8",
  "Go-http-client/1.1"
]

PATHS = %w[/api/v1/users /login /dashboard /settings /api/v1/metrics /health]

generate_sample = -> {
  {
    request_id: SecureRandom.hex(12),
    timestamp: (Time.now - rand(0..86400)).iso8601,
    http: {
      method: [ "GET", "POST", "PUT" ].sample,
      path: PATHS.sample,
      status: [ 200, 200, 201, 400, 403, 500 ].sample,
      user_agent: USER_AGENTS.sample,
      latency_ms: rand(10.5..500.0).round(2),
    },
    message: Array.new(rand(5..15)) { DICTIONARY.sample }.join(" "),
    meta: {
      shard: "db-shard-#{rand(1..5)}",
      region: [ "us-east-1", "eu-central-1" ].sample,
      retries: rand(0..3),
    },
  }
}

# Generate ~5MB of JSON data
INPUT_DATA = 5000.times.map { generate_sample.call }.to_json
original_kb = INPUT_DATA.bytesize / 1024.0

puts "Original Data Size: #{original_kb.round(2)} KB"
puts "---------------------------------------------------"

# --- 2. Prepare Compressed Payloads ---
payloads = {}

# LZ4
payloads['LZ4'] = LZ4.compress(INPUT_DATA)

# Zstd
payloads['Zstd-Fast (-5)'] = Zstd.compress(INPUT_DATA, level: -5)
payloads['Zstd-Fast (-3)'] = Zstd.compress(INPUT_DATA, level: -3)
payloads['Zstd-Level 1']   = Zstd.compress(INPUT_DATA, level: 1)
payloads['Zstd-Default (3)'] = Zstd.compress(INPUT_DATA, level: 3)
payloads['Zstd-Level 5']   = Zstd.compress(INPUT_DATA, level: 5)

# Zlib
payloads['Zlib-Level 1'] = Zlib::Deflate.deflate(INPUT_DATA, Zlib::BEST_SPEED)
payloads['Zlib-Def (6)'] = Zlib::Deflate.deflate(INPUT_DATA, Zlib::DEFAULT_COMPRESSION)

# --- 3. Report Sizes ---
puts "COMPRESSION SIZE REPORT (KB):"
puts format("%-18s | %-12s | %-10s", "Algorithm", "Size (KB)", "Ratio")
puts "-" * 50

payloads.each do |name, data|
  size_kb = data.bytesize / 1024.0
  ratio = (INPUT_DATA.bytesize.to_f / data.bytesize).round(2)
  puts format("%-18s | %-12s | %sx", name, size_kb.round(2), ratio)
end
puts "---------------------------------------------------"

# --- 4. Benchmark Compression Speed ---
puts "\nBENCHMARK: COMPRESSION SPEED (Higher i/s is better)"
Benchmark.ips do |x|
  x.config(time: 5, warmup: 2)

  x.report("LZ4")              { LZ4.compress(INPUT_DATA) }
  x.report("Zstd-Fast (-5)")   { Zstd.compress(INPUT_DATA, level: -5) }
  x.report("Zstd-Fast (-3)")   { Zstd.compress(INPUT_DATA, level: -3) }
  x.report("Zstd-Level 1")     { Zstd.compress(INPUT_DATA, level: 1) }
  x.report("Zstd-Default (3)") { Zstd.compress(INPUT_DATA, level: 3) }
  x.report("Zstd-Level 5")     { Zstd.compress(INPUT_DATA, level: 5) }
  x.report("Zlib-Level 1")     { Zlib::Deflate.deflate(INPUT_DATA, Zlib::BEST_SPEED) }
  x.report("Zlib-Def (6)")     { Zlib::Deflate.deflate(INPUT_DATA, Zlib::DEFAULT_COMPRESSION) }

  x.compare!
end

# --- 5. Benchmark Decompression Speed ---
puts "\nBENCHMARK: DECOMPRESSION SPEED (Higher i/s is better)"
Benchmark.ips do |x|
  x.config(time: 5, warmup: 2)

  x.report("LZ4")              { LZ4.decompress(payloads['LZ4']) }
  x.report("Zstd-Fast (-5)")   { Zstd.decompress(payloads['Zstd-Fast (-5)']) }
  x.report("Zstd-Fast (-3)")   { Zstd.decompress(payloads['Zstd-Fast (-3)']) }
  x.report("Zstd-Level 1")     { Zstd.decompress(payloads['Zstd-Level 1']) }
  x.report("Zstd-Default (3)") { Zstd.decompress(payloads['Zstd-Default (3)']) }
  x.report("Zstd-Level 5")     { Zstd.decompress(payloads['Zstd-Level 5']) }
  x.report("Zlib-Level 1")     { Zlib::Inflate.inflate(payloads['Zlib-Level 1']) }
  x.report("Zlib-Def (6)")     { Zlib::Inflate.inflate(payloads['Zlib-Def (6)']) }

  x.compare!
end
