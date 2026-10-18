# frozen_string_literal: true

module McpTools
  # A Log as query_logs answers it: one line of its time, Level, Service,
  # id, message and first Attributes, so a page of Logs stays small.
  # get_log answers the whole Log.
  module LogLine
    MESSAGE_CHARS = 500
    ATTRIBUTES = 8
    VALUE_CHARS = 100
    # What a line holds, in order, for the header of a page of lines.
    PARTS = "time, level, service, id, message, then up to #{ATTRIBUTES} Attributes as name=value".freeze
    # An Attribute name written as it is; any other is quoted.
    PLAIN_NAME = /\A[\w.@-]+\z/

    # The line of a Log as EventStore answers it. A missing Level or Service
    # is "-". The message and string values are quoted, so a string "200"
    # reads apart from the number 200, and cut with "…" when long.
    def self.of(log)
      attributes = log["attributes"].is_a?(Hash) ? log["attributes"] : {}
      parts = [ log["time"], log["level"].presence || "-", log["service"].presence || "-", log["id"],
                JSON.generate(cut(log["message"].to_s, MESSAGE_CHARS)) ]
      parts += attributes.first(ATTRIBUTES).map { |name, value| "#{name_of(name)}=#{value_of(value)}" }
      parts << "+#{attributes.size - ATTRIBUTES} more" if attributes.size > ATTRIBUTES
      parts.join(" ")
    end

    def self.name_of(name)
      PLAIN_NAME.match?(name) ? name : JSON.generate(name)
    end

    # A value as JSON: a string cut before it's quoted, and an array or an
    # object cut after.
    def self.value_of(value)
      return JSON.generate(cut(value, VALUE_CHARS)) if value.is_a?(String)

      cut(JSON.generate(value), VALUE_CHARS)
    end

    def self.cut(text, chars)
      text.length > chars ? "#{text[0, chars]}…" : text
    end
  end
end
