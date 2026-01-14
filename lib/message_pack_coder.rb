# frozen_string_literal: true

require "msgpack"
require "zlib"

module MessagePackCoder
  @pool = ::MessagePack::Factory.new.pool(ENV.fetch("RAILS_MAX_THREADS", 5).to_i)

  class << self
    delegate :dump, :load, to: :@pool
  end

  module WithCompression
    THRESHOLD = 10.kilobytes

    # Single byte markers to identify the payload type
    MARKER_RAW        = [ 0 ].pack("C")
    MARKER_COMPRESSED = [ 1 ].pack("C")

    # Effort options:
    # Zlib::BEST_SPEED -> 1 (~24x slower than uncompressed MessagePack)
    # Zlib::DEFAULT_COMPRESSION -> 6 (~66x slower)
    # Zlib::BEST_COMPRESSION -> 9 (~235x slower)
    def self.dump(object, compression_threshold: THRESHOLD, effort: Zlib::DEFAULT_COMPRESSION)
      packed = MessagePackCoder.dump(object)

      if packed.bytesize >= compression_threshold
        compressed = Zlib::Deflate.deflate(packed, effort)
        return MARKER_COMPRESSED + compressed
      end

      MARKER_RAW + packed
    end

    def self.load(binary_data)
      return nil if binary_data.nil?

      # Check the first byte to determine strategy
      if binary_data.start_with?(MARKER_COMPRESSED)
        # Strip marker (byte 0) and inflate the rest
        decoded = Zlib::Inflate.inflate(binary_data.byteslice(1..-1))
        MessagePackCoder.load(decoded)
      elsif binary_data.start_with?(MARKER_RAW)
        # Strip marker (byte 0) and load raw
        MessagePackCoder.load(binary_data.byteslice(1..-1))
      end
    end
  end
end
