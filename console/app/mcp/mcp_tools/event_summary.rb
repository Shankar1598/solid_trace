# frozen_string_literal: true

module McpTools
  # An Issue's Event as get_issue answers it: what the Issue page shows of
  # its Sentry payload, cut short. Senders write payloads, so every part is
  # read as it may or may not be there.
  module EventSummary
    EXCEPTIONS = 5
    # The frames kept per exception, from the frame that raised.
    FRAMES = 10
    TAGS = 50
    LONGEST = 500

    # {id, time, environment, level, release, message, exceptions, tags,
    # trace_id}, leaving out what the Event lacks, or saying what's missing
    # when EventStore didn't give its payload. Each exception, the one
    # raised first and then its causes, has its type, value and top in-app
    # frames, or its top frames when none is in-app.
    def self.compact(event)
      payload = event.payload
      unless payload.is_a?(Hash)
        return indexed(event).merge(missing: "EventStore didn't give this Event's payload, so its exception is left out. Try again shortly.")
      end

      indexed(event).merge(
        level: text(payload["level"]),
        release: text(payload["release"]),
        message: message(payload),
        exceptions: exceptions(payload).presence,
        tags: tags(payload["tags"]).presence || tags(event.tags).presence,
        trace_id: trace_id(payload),
      ).compact
    end

    # The trace id the Event's trace context holds, or nil.
    def self.trace_id(payload)
      trace = payload["contexts"].is_a?(Hash) ? payload["contexts"]["trace"] : nil
      text(trace["trace_id"]) if trace.is_a?(Hash)
    end

    # What EventStore's index holds for every Event.
    def self.indexed(event)
      { id: event.uuid, time: event.timestamp&.utc&.iso8601(3), environment: event.environment.presence, tags: tags(event.tags).presence }.compact
    end

    def self.message(payload)
      entry = payload["logentry"] || payload["message"]
      text(entry.is_a?(Hash) ? entry["formatted"] || entry["message"] : entry)
    end

    # Sentry lists an exception's chain cause first, so the one raised is
    # last.
    def self.exceptions(payload)
      exception = payload["exception"]
      values = exception.is_a?(Hash) ? exception["values"] : exception
      Array(values).grep(Hash).reverse.first(EXCEPTIONS).map do |value|
        { type: text(value["type"]), value: text(value["value"]), frames: frames(value["stacktrace"]).presence }.compact
      end
    end

    def self.frames(stacktrace)
      frames = stacktrace.is_a?(Hash) ? Array(stacktrace["frames"]).grep(Hash).reverse : []
      in_app = frames.select { |frame| frame["in_app"] == true }
      (in_app.presence || frames).first(FRAMES).map do |frame|
        {
          filename: text(frame["filename"] || frame["abs_path"] || frame["module"]),
          lineno: frame["lineno"].is_a?(Integer) ? frame["lineno"] : nil,
          function: text(frame["function"]),
          context_line: text(frame["context_line"])&.strip,
        }.compact
      end
    end

    # Sentry sends tags as an object or as [key, value] pairs.
    def self.tags(tags)
      pairs = tags.is_a?(Hash) ? tags.to_a : Array(tags).select { |pair| pair.is_a?(Array) && pair.size == 2 }
      pairs.first(TAGS).to_h { |key, value| [ key.to_s, text(value) ] }
    end

    # A value as text, cut at LONGEST characters, or nil when it's blank.
    def self.text(value)
      value = value.to_s unless value.nil? || value.is_a?(String)
      value.presence&.truncate(LONGEST)
    end
  end
end
