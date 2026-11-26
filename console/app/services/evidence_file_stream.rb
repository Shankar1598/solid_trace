# frozen_string_literal: true

class EvidenceFileStream
  class Busy < StandardError; end

  class OutsideTheFile < StandardError
    attr_reader :content_range

    def initialize(content_range)
      @content_range = content_range
      super("The range is outside the Evidence file")
    end
  end

  AT_ONCE = 4
  SLOT_WAIT_SECONDS = 1
  WRITE_SECONDS = 10
  STREAM_SECONDS = 600

  @lock = Mutex.new
  @slot_freed = ConditionVariable.new
  @streaming = 0

  class << self
    def open(id:, range: nil)
      take_slot
      stream = new(id, range)
      begin
        stream.read_answer
      rescue StandardError
        stream.close
        raise
      end
    end

    def take_slot
      deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + SLOT_WAIT_SECONDS
      @lock.synchronize do
        while @streaming >= AT_ONCE
          wait = deadline - Process.clock_gettime(Process::CLOCK_MONOTONIC)
          raise Busy if wait <= 0

          @slot_freed.wait(@lock, wait)
        end
        @streaming += 1
      end
    end

    def free_slot
      @lock.synchronize do
        @streaming -= 1
        @slot_freed.signal
      end
    end
  end

  def initialize(id, range)
    @id = id
    @reads = Fiber.new do
      EventStore.evidence_content(id: id, range: range) do |answer|
        Fiber.yield answer
        answer.read_body { |chunk| Fiber.yield chunk }
      end
      nil
    end
  end

  def read_answer
    @answer = @reads.resume
    case status
    when 200, 206
      raise EventStore::Unavailable, "EventStore gave no Content-Length for Evidence file #{@id}" unless @answer["Content-Length"]
    when 416 then raise OutsideTheFile, @answer["Content-Range"]
    else raise EventStore::Unavailable, "EventStore answered #{@answer.code} for Evidence file #{@id}"
    end
    @first_chunk = @reads.resume
    self
  end

  def status
    @answer.code.to_i
  end

  def headers
    %w[Content-Length Content-Range Accept-Ranges].index_with { |name| @answer[name] }.compact
  end

  def call(socket)
    socket.timeout = WRITE_SECONDS if socket.respond_to?(:timeout=)
    stop_at = Process.clock_gettime(Process::CLOCK_MONOTONIC) + STREAM_SECONDS
    chunk = @first_chunk
    while chunk
      socket.write(chunk)
      if Process.clock_gettime(Process::CLOCK_MONOTONIC) > stop_at
        Rails.logger.info("Evidence file bytes: the browser read for more than #{STREAM_SECONDS} s, so the stream stopped")
        break
      end
      chunk = @reads.resume
    end
  rescue EventStore::Unavailable => e
    Rails.logger.error("Evidence file bytes stopped before their Content-Length: #{e.message}")
  rescue IOError, SystemCallError => e
    Rails.logger.info("Evidence file bytes: the browser closed the connection or stopped reading: #{e.message}")
  ensure
    socket.close
    close
  end

  def close
    return if @closed

    @closed = true
    @reads.kill if @reads.alive?
    self.class.free_slot
  end
end
