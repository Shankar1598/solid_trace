# frozen_string_literal: true

module McpTools
  # What the Log transform tools share: how a program is written, the program
  # argument, a version as they answer it, and VRL's errors. Any member reads
  # and previews a Project's Log transform; only its Organization's admins
  # change it (LogTransformSave), as on its page.
  module LogTransformTool
    extend ActiveSupport::Concern

    # How a program is written and what Log processing does with it, from the
    # spec, ticket 02's decisions and ADR 0012's left-out functions.
    LANGUAGE = <<~TEXT.squish
      A Project's Log transform is a VRL program, the language of Vector's remap
      (functions: https://vector.dev/docs/reference/vrl/functions/), which Log processing runs on each of the Project's Logs before storing it.
      . is one Log as it arrived: the built-in fields at the top, .time a timestamp and .message, .level, .service,
      .environment, .trace_id and .span_id strings, each absent when the Log has none;
      and the Attributes nested by their dots, so http.method is .http.method, or ."a.b" when a shorter name a holds a value.
      Numbers, strings and bools keep their types, and an array is an array.
      A program sets, changes and deletes Fields, such as .status = parsed.status then del(.message).
      What . holds at the end is stored, flattened to dotted Attributes and checked as a sender's Log is:
      only those names fill built-in fields, an unknown Level becomes original.level, a solidtrace. name it sets becomes
      original.solidtrace., and the solidtrace. Attributes a Log arrived with can't be changed.
      abort drops the Log, which is never stored.
      A Log the program fails on, by an error or by leaving . not an object, is stored as it arrived, with the error in
      the Attribute solidtrace.transform_error; query_logs finds them with where isnotnull(solidtrace.transform_error).
      The network, environment and host functions (http_request, dns_lookup, reverse_dns, get_env_var, get_hostname and their kin)
      and decode_gzip, decode_zlib, decode_zstd and decode_snappy are missing: calling one doesn't compile.
      Programs run in UTC. A program is at most 64 KB, and an empty one is none.
      A save applies to the Logs processed after it, within seconds: Logs already stored don't change.
    TEXT

    PROGRAM = { type: "string", description: "The VRL program, at most 64 KB. An empty one is none." }.freeze

    private

    # A version's id, who saved it (nil once they're gone) and when.
    def version_json(version)
      saver = version.user
      { version: version.id, saved_by: saver && { name: saver.name, email: saver.email },
        saved_at: version.created_at.utc.iso8601 }
    end

    # The tool error for a program that doesn't compile: VRL's formatted
    # errors, one after another.
    def compile_errors!(lead, errors)
      raise ToolError, [ lead, *errors.map { |error| error["text"].presence || error["message"].to_s } ].join("\n")
    end

    def log_transform_link
      links.log_transform(project)
    end
  end
end
