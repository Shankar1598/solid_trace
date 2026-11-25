# frozen_string_literal: true

# Un-minifies the stack frames of the frontend's errors before Console sends
# them to its Sentry (BrowserErrorsController, docs/traces.md). The production
# build writes a source map next to each bundle (vite.config.mts), so Console
# needs no upload step: a frame in /vite/assets/<bundle>.js gets the file, line,
# column and enclosing function of Console's own source.
#
# This also keeps an error one Issue across releases: EventStore groups by the
# culprit, the top in-app frame's function, file and line, and a bundle's name
# and its minified names change with each build.
module BrowserSourceMaps
  # The decoded maps kept, as each is read in full once: one bundle's map is
  # about 20 MB.
  KEPT = 2
  # Puma's threads share the maps kept.
  LOCK = Mutex.new

  Position = Data.define(:source, :line, :column, :function, :context_line)

  class << self
    # The envelope with each event's frames un-minified, or as it is when it
    # has none to change or can't be read: a report is never lost.
    def rewrite_envelope(body, public_dir: Rails.public_path, output_dir: ViteRuby.config.public_output_dir)
      Envelope.new(body).rewrite { |event| rewrite_event(event, public_dir, output_dir) }
    rescue StandardError => e
      Rails.logger.warn("BrowserSourceMaps couldn't read a browser's envelope: #{e.class}: #{e.message}")
      body
    end

    # Forgets the maps kept, as a test with other maps needs.
    def reset
      LOCK.synchronize { @maps = nil }
    end

    private

    # The event with its frames un-minified, or nil when none changed.
    def rewrite_event(event, public_dir, output_dir)
      frames = Array(event["exception"].is_a?(Hash) ? event["exception"]["values"] : nil)
        .flat_map { |value| Array(value.is_a?(Hash) ? value.dig("stacktrace", "frames") : nil) }
      changed = frames.count { |frame| frame.is_a?(Hash) && rewrite_frame(frame, public_dir, output_dir) }
      event if changed.positive?
    end

    def rewrite_frame(frame, public_dir, output_dir)
      map = map_for(frame["abs_path"] || frame["filename"], public_dir, output_dir)
      position = map&.position(frame["lineno"], frame["colno"])
      return unless position

      frame["abs_path"] ||= frame["filename"]
      frame["raw_function"] = frame["function"] if frame["function"]
      frame["filename"] = position.source
      frame["lineno"] = position.line
      frame["colno"] = position.column
      position.function ? frame["function"] = position.function : frame.delete("function")
      frame["context_line"] = position.context_line if position.context_line
      frame["in_app"] = !position.source.start_with?("node_modules/")
      true
    end

    # The map of a bundle URL of the production build, or nil for any other
    # URL, such as Vite's dev server's.
    def map_for(url, public_dir, output_dir)
      path = URI.parse(url.to_s).path.to_s
      return unless path.match?(%r{\A/#{Regexp.escape(output_dir)}/assets/[\w.-]+\.js\z})

      file = File.join(public_dir.to_s, "#{path}.map")
      return unless File.file?(file)

      key = [ file, File.mtime(file) ]
      LOCK.synchronize do
        @maps ||= {}
        @maps[key] ||= begin
          @maps.shift while @maps.size >= KEPT
          SourceMap.new(File.read(file), root: File.dirname(public_dir.to_s), dir: File.dirname(file))
        end
      end
    rescue URI::InvalidURIError
      nil
    end
  end

  # A Sentry envelope: a header line, then items, each a header line and a
  # payload, of the header's length or up to the next newline.
  class Envelope
    def initialize(body)
      @original = body
      @body = body.b
    end

    # The envelope with each event item's payload as the block returns it,
    # or the item as it was when the block returns nil. The envelope itself
    # when no item changed.
    def rewrite
      io = StringIO.new(@body)
      out = +"".b
      out << (io.gets || "")
      changed = false
      until io.eof?
        from = io.pos
        line = io.gets
        next out << line if line.strip.empty?

        header = JSON.parse(line)
        payload = header["length"] ? io.read(header["length"]) : io.gets.to_s.chomp
        if header["length"]
          newline = io.getc
          io.ungetc(newline) if newline && newline != "\n"
        end
        event = header["type"] == "event" ? yield(JSON.parse(payload)) : nil
        next out << @body.byteslice(from...io.pos) unless event

        changed = true
        payload = JSON.generate(event).b
        out << JSON.generate(header.merge("length" => payload.bytesize)) << "\n" << payload << "\n"
      end
      changed ? out : @original
    end
  end

  # One version 3 source map. Its mappings are decoded once, to the state at
  # the start of each generated line, so a lookup decodes only its own line.
  class SourceMap
    BASE64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
      .each_char.with_index.to_h { |char, i| [ char.ord, i ] }.freeze
    COMMA = ",".ord
    SEMICOLON = ";".ord

    def initialize(json, root:, dir:)
      map = JSON.parse(json)
      @mappings = map.fetch("mappings")
      @names = map.fetch("names", [])
      @contents = map.fetch("sourcesContent", [])
      @sources = map.fetch("sources").map { |source| local_path(source, map["sourceRoot"], root, dir) }
      @line_starts = index_lines
    end

    # The original position of a generated line and column, both from 1 as
    # Sentry's frames count them, or nil when the map has none there.
    def position(line, column)
      return unless line.is_a?(Integer) && column.is_a?(Integer) && line >= 1 && column >= 1
      start = @line_starts[line - 1]
      return unless start

      segment = segment_at(start, column - 1)
      return unless segment

      source, source_line, source_column = segment
      content = @contents[source]
      Position.new(
        source: @sources[source],
        line: source_line + 1,
        column: source_column + 1,
        function: content && EnclosingFunction.name(content, source_line, source_column),
        context_line: content&.lines&.[](source_line)&.chomp&.strip&.slice(0, 300)
      )
    end

    private

    # Sources as paths from Console's root, as "app/frontend/..." or
    # "node_modules/...".
    def local_path(source, source_root, root, dir)
      full = File.expand_path(source_root.present? ? File.join(source_root, source) : source, dir)
      full.start_with?("#{root}/") ? full.delete_prefix("#{root}/") : source
    end

    # For each generated line: where its segments start in the mappings, and
    # the source, line, column and name the segments before it left.
    def index_lines
      starts = [ [ 0, 0, 0, 0, 0 ] ]
      state = [ 0, 0, 0, 0 ]
      field = 0
      value = 0
      shift = 0
      @mappings.each_byte.with_index do |byte, i|
        if byte == SEMICOLON || byte == COMMA
          field = 0
          starts << [ i + 1, *state ] if byte == SEMICOLON
          next
        end
        digit = BASE64.fetch(byte)
        value += (digit & 31) << shift
        if digit & 32 != 0
          shift += 5
          next
        end
        delta = value & 1 == 1 ? -(value >> 1) : value >> 1
        state[field - 1] += delta if field >= 1 && field <= 4
        field += 1
        value = 0
        shift = 0
      end
      starts
    end

    # The source, line and column of the last segment of the line that starts
    # at or before column, from the line's start state.
    def segment_at(start, column)
      offset, source, source_line, source_column, = start
      found = nil
      generated = 0
      fields = []
      value = 0
      shift = 0
      i = offset
      while i <= @mappings.bytesize
        byte = @mappings.getbyte(i)
        if byte.nil? || byte == COMMA || byte == SEMICOLON
          unless fields.empty?
            generated += fields[0]
            break if generated > column
            if fields.size >= 4
              source += fields[1]
              source_line += fields[2]
              source_column += fields[3]
              found = [ source, source_line, source_column ]
            end
          end
          break if byte.nil? || byte == SEMICOLON
          fields = []
        else
          digit = BASE64.fetch(byte)
          value += (digit & 31) << shift
          if digit & 32 != 0
            shift += 5
          else
            fields << (value & 1 == 1 ? -(value >> 1) : value >> 1)
            value = 0
            shift = 0
          end
        end
        i += 1
      end
      found
    end
  end

  # The name of the function around a position in source code, found by
  # walking back to each unclosed "{" and reading the declaration that ends
  # just before it: function Name(...), const Name = (...) =>, const Name =
  # param =>, or a method name(...). An anonymous callback, such as
  # useEffect(() => {, gives the function around it. nil when none is found,
  # as at a module's top level.
  module EnclosingFunction
    NAME = /[A-Za-z_$][\w$]*/
    # Each ends at the "(" of its parameters.
    WITH_PARAMETERS = [
      /\bfunction\s*\*?\s*(#{NAME})\s*(?:<[^()]*>)?\s*\(/,
      /\b(?:const|let|var)\s+(#{NAME})\s*(?::[^=]*)?=\s*(?:async\s*)?(?:function\s*\*?\s*(?:#{NAME})?\s*)?(?:<[^()]*>)?\(/,
      /(?:\A|[\s;,{}])(?:async\s+)?(?!(?:if|for|while|switch|catch|return|function|await|typeof)\b)(#{NAME})\s*\(/
    ].freeze
    ONE_PARAMETER = /\b(?:const|let|var)\s+(#{NAME})\s*=\s*(?:async\s+)?#{NAME}\s*\z/
    # What may come between a declaration's ")" and its "{": a return type.
    TAIL = /\A\s*(?::[^;]*)?\z/

    # line and column from 0.
    def self.name(content, line, column)
      lines = content.lines
      return unless lines[line]

      before = lines[0...line].join + lines[line][0, column].to_s
      depth = 0
      i = before.length - 1
      while i >= 0
        case before[i]
        when "}", ")", "]" then depth += 1
        when "{", "(", "["
          if depth.zero?
            name = before[i] == "(" ? parameters_of(before, i) : (declared_before(before, i) if before[i] == "{")
            return name if name
          else
            depth -= 1
          end
        end
        i -= 1
      end
      nil
    end

    # The name whose declaration's parameter list starts at the "(" at index
    # i, for a position inside the parameters, such as a destructured prop.
    # Only function and const declarations: a method's form, name(, is also a
    # call's, such as useEffect(.
    def self.parameters_of(text, i)
      line_start = text.rindex("\n", i - 1) || -1
      head = text[(line_start + 1)..i]
      WITH_PARAMETERS.first(2).each do |pattern|
        match = head.match(/(?:#{pattern.source})\z/)
        return match[1] if match
      end
      nil
    end

    # The name declared just before the "{" at index i, in the text from the
    # line before.
    def self.declared_before(text, i)
      line_start = text.rindex("\n", i - 1) || -1
      previous_start = line_start.positive? ? (text.rindex("\n", line_start - 1) || -1) : line_start
      head = text[(previous_start + 1)...i].gsub(/\s+/, " ").rstrip.delete_suffix("=>").rstrip
      return Regexp.last_match(1) if head.match(ONE_PARAMETER)

      WITH_PARAMETERS.each do |pattern|
        candidates = []
        head.scan(pattern) { candidates << [ Regexp.last_match(1), Regexp.last_match.end(0) - 1 ] }
        candidates.reverse_each do |name, open|
          close = closing(head, open)
          return name if close && head[(close + 1)..].match?(TAIL)
        end
      end
      nil
    end

    # The index of the ")" that closes the "(" at open, or nil.
    def self.closing(text, open)
      depth = 0
      (open...text.length).each do |i|
        case text[i]
        when "(", "[", "{" then depth += 1
        when ")", "]", "}"
          depth -= 1
          return i if depth.zero?
        end
      end
      nil
    end
    private_class_method :declared_before, :parameters_of, :closing
  end
end
