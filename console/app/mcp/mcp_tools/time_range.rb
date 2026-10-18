# frozen_string_literal: true

module McpTools
  # The time range a tool reads: range, a span back from now such as 15m, 1h,
  # 24h or 7d (1h when none is given), or ISO 8601 from and to. A span is a
  # whole number of minutes, hours or days, ten years at most, as the
  # explorer's are (lib/timeRange.ts), and stays a span in a Console link.
  class TimeRange
    SPAN = /\A(\d+)([mhd])\z/
    UNITS = { "m" => 1.minute, "h" => 1.hour, "d" => 1.day }.freeze
    DEFAULT_SPAN = "1h"
    LONGEST = 3650.days

    attr_reader :from, :to, :span

    # Raises ToolError, saying what to give instead, for a range it can't read.
    def self.parse(range: nil, from: nil, to: nil, now: Time.current)
      if from.present? || to.present?
        raise ToolError, "Give range, or from and to, not both." if range.present?
        raise ToolError, "Give both from and to." unless from.present? && to.present?

        from, to = [ from, to ].map { |time| iso8601(time) }
        raise ToolError, "from must be before to." unless from < to

        new(from: from, to: to)
      else
        span = range.presence || DEFAULT_SPAN
        new(from: now - span_length(span), to: now, span: span)
      end
    end

    def self.span_length(span)
      number, unit = SPAN.match(span.to_s)&.captures
      length = number.to_i * UNITS.fetch(unit, 0)
      return length if length.positive? && length <= LONGEST

      raise ToolError, "range must be a span back from now such as 15m, 1h, 24h or 7d: a whole number of m, h or d, ten years at most."
    end

    def self.iso8601(time)
      Time.iso8601(time.to_s)
    rescue ArgumentError
      raise ToolError, "from and to must be ISO 8601 times, such as 2026-10-17T09:00:00Z."
    end

    def initialize(from:, to:, span: nil)
      @from = from
      @to = to
      @span = span
    end

    # The range as a Console link's search holds it: range for a span, or
    # from and to as UTC ISO times.
    def link_params
      span ? { range: span } : { from: from.utc.iso8601(3), to: to.utc.iso8601(3) }
    end
  end
end
