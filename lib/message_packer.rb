# frozen_string_literal: true

require "msgpack"
require "zstd-ruby"

module MessagePacker
  @pool = ::MessagePack::Factory.new.pool(ENV.fetch("RAILS_MAX_THREADS", 5).to_i)

  class << self
    delegate :dump, :load, to: :@pool
  end

  class WithCompression
    THRESHOLD = 10.kilobytes

    # Single byte markers to identify the payload type
    MARKER_RAW  = [ 0 ].pack("C")
    MARKER_ZSTD = [ 1 ].pack("C")

    @default_zstd_level = 3

    # Zstd level 3 is a good balance for mid or long term storage.
    # Lower levels (e.g., -3 to 1) are faster but compress less.
    # Higher levels (e.g., 5-19) compress more but are slower.
    def self.dump(object, compression_threshold: THRESHOLD, level: @default_zstd_level)
      packed = MessagePacker.dump(object)

      return MARKER_RAW + packed if packed.bytesize < compression_threshold

      compressed = Zstd.compress(packed, level: level)
      MARKER_ZSTD + compressed
    end

    def self.load(binary_data)
      return nil if binary_data.nil?
      return nil if binary_data.empty?

      marker = binary_data[0]
      data = binary_data[1..-1]

      case marker
      when MARKER_ZSTD
        decoded = Zstd.decompress(data)
        MessagePacker.load(decoded)
      when MARKER_RAW
        MessagePacker.load(data)
      else
        raise ArgumentError, "Unknown marker: #{marker.inspect}"
      end
    end
  end

  class WithCompression::VeryFast < WithCompression
    @default_zstd_level = -3
  end
end
