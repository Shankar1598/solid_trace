require "test_helper"
require "message_packer"

class MessagePackerTest < ActiveSupport::TestCase
  test "dumps and loads raw data when below threshold" do
    data = { "hello" => "world" }
    packed = MessagePacker::WithCompression.dump(data, compression_threshold: 100)
    assert_equal 0, packed.bytes[0] # MARKER_RAW

    loaded = MessagePacker::WithCompression.load(packed)
    assert_equal data, loaded
  end

  test "compresses with LZ4 when above threshold and mode is LZ4" do
    data = { "a" => "a" * 100 }
    packed = MessagePacker::WithCompression.dump(data, mode: :lz4, compression_threshold: 10)
    assert_equal 1, packed.bytes[0] # MARKER_LZ4

    loaded = MessagePacker::WithCompression.load(packed)
    assert_equal data, loaded
  end

  test "compresses with Zstd when above threshold and mode is Zstd" do
    data = { "a" => "a" * 100 }
    packed = MessagePacker::WithCompression.dump(data, mode: :zstd, compression_threshold: 10)
    assert_equal 2, packed.bytes[0] # MARKER_ZSTD

    loaded = MessagePacker::WithCompression.load(packed)
    assert_equal data, loaded
  end

  test "respects Zstd levels" do
    data = { "a" => "a" * 1000 }
    packed = MessagePacker::WithCompression.dump(data, mode: :zstd, compression_threshold: 10, options: { level: 1 })
    loaded = MessagePacker::WithCompression.load(packed)
    assert_equal data, loaded
  end
end
