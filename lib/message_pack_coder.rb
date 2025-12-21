# frozen_string_literal: true

require "msgpack"

module MessagePackCoder
  POOL = ::MessagePack::Factory.new.pool(ENV.fetch("RAILS_MAX_THREADS", 5).to_i)

  def self.dump(object)
    POOL.packer do |packer|
      packer.write(object)
      packer.full_pack
    end
  end

  def self.load(binary_data)
    return nil if binary_data.nil?
    POOL.unpacker do |unpacker|
      unpacker.feed_reference(binary_data)
      unpacker.full_unpack
    end
  end

  module Compressed
    def self.dump(object)
      Zlib::Deflate.deflate(MessagePackCoder.dump(object))
    end

    def self.load(binary_data)
      return nil if binary_data.nil?
      MessagePackCoder.load(Zlib::Inflate.inflate(binary_data))
    end
  end
end
