# frozen_string_literal: true

require "msgpack"
require "lz4-ruby"
require "zstd-ruby"

module MessagePacker
  @pool = ::MessagePack::Factory.new.pool(ENV.fetch("RAILS_MAX_THREADS", 5).to_i)

  class << self
    delegate :dump, :load, to: :@pool
  end

  module WithCompression
    THRESHOLD = 10.kilobytes

    # Single byte markers to identify the payload type
    MARKER_RAW  = [ 0 ].pack("C")
    MARKER_LZ4  = [ 1 ].pack("C")
    MARKER_ZSTD = [ 2 ].pack("C")

    # LZ4 is best for read heavy workload. It is optimised for easy decompression
    # ZSTD level -3 is best ballance for short term storage (both compression and decompression is fast)
    # Zstd level 3 is best for mid or long term storage as compression takes some CPU
    def self.dump(object, mode: :zstd, compression_threshold: THRESHOLD, options: { level: 3 })
      packed = MessagePacker.dump(object)

      return MARKER_RAW + packed if packed.bytesize < compression_threshold

      case mode
      when :lz4
        compressed = LZ4.compress(packed)
        MARKER_LZ4 + compressed
      when :zstd
        level = options[:level] || 3
        compressed = Zstd.compress(packed, level: level)
        MARKER_ZSTD + compressed
      else
        raise ArgumentError, "Unknown compression mode: #{mode}"
      end
    end

    def self.load(binary_data)
      return nil if binary_data.nil?
      return nil if binary_data.empty?

      marker = binary_data[0]
      data = binary_data[1..-1]

      case marker
      when MARKER_LZ4
        decoded = LZ4.decompress(data)
        MessagePacker.load(decoded)
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
end
