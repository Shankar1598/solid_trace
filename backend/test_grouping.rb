# Test the grouping logic in Rails console

# Test 1: Basic grouping with default fingerprint
puts "Test 1: Computing hash with default fingerprint"
hash1 = Issue.compute_hash(
  title: "Database connection failed",
  culprit: "app/models/user.rb:42",
  event_type: "error"
)
puts "Hash: #{hash1}"

# Test 2: Same event should produce same hash
puts "\nTest 2: Same event should produce same hash"
hash2 = Issue.compute_hash(
  title: "Database connection failed",
  culprit: "app/models/user.rb:42",
  event_type: "error"
)
puts "Hash: #{hash2}"
puts "Hashes match: #{hash1 == hash2}"

# Test 3: Different culprit should produce different hash
puts "\nTest 3: Different culprit produces different hash"
hash3 = Issue.compute_hash(
  title: "Database connection failed",
  culprit: "app/models/post.rb:15",
  event_type: "error"
)
puts "Hash: #{hash3}"
puts "Same as first: #{hash1 == hash3}"

# Test 4: Custom fingerprint with {{ default }}
puts "\nTest 4: Custom fingerprint with {{ default }}"
hash4 = Issue.compute_hash(
  title: "Database connection failed",
  culprit: "app/models/user.rb:42",
  event_type: "error",
  fingerprint: ["{{ default }}", "production"]
)
puts "Hash: #{hash4}"
puts "Same as first: #{hash1 == hash4}"

# Test 5: Custom fingerprint overriding default
puts "\nTest 5: Custom fingerprint overriding default (ignore culprit/title)"
hash5 = Issue.compute_hash(
  title: "Any error message",
  culprit: "any/file.rb:1",
  event_type: "error",
  fingerprint: ["custom_group_id"]
)
puts "Hash: #{hash5}"

# Test 6: Another event with same custom fingerprint
hash6 = Issue.compute_hash(
  title: "Different error message",
  culprit: "different/file.rb:99",
  event_type: "warning",
  fingerprint: ["custom_group_id"]
)
puts "Hash: #{hash6}"
puts "Custom fingerprints match: #{hash5 == hash6}"
