# frozen_string_literal: true

class Utils
  class << self
    def profile
      start = Time.now
      result = yield
      puts "Time taken: #{Time.now - start} seconds"
      result
    end
  end
end
