# frozen_string_literal: true

class MessagePackCoder
  def self.dump(obj)
    return nil if obj.nil?

    MessagePack.pack(obj)
  end

  def self.load(data)
    return {} if data.nil?

    MessagePack.unpack(data)
  end
end
