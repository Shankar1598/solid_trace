# frozen_string_literal: true

require "json"
require "net/http"
require "securerandom"

# Dummy Logs for development: sends a Project made-up Logs through EventStore's
# Log ingest, as a shipper or a Sentry SDK would. Most go to the JSON route as
# NDJSON; a few go as Sentry envelope `log` items. The senders' key spellings
# vary (msg/ts/severity, nested objects) so normalisation has work to do.
namespace :dev do
  desc "Send dummy Logs to EventStore. PROJECT=<slug> (default: first Project), COUNT=20000, HOURS=24, " \
    "INGEST_URL=http://localhost:4000, SEED=<int>"
  task logs: :environment do
    abort "dev:logs is for development only" if Rails.env.production?

    project = ENV["PROJECT"] ? Project.find_by!(slug: ENV["PROJECT"]) : Project.order(:id).first!
    key = project.project_keys.first&.public_key or abort "Project #{project.slug} has no key"
    count = Integer(ENV.fetch("COUNT", "20000"))
    hours = Integer(ENV.fetch("HOURS", "24"))
    ingest = URI(ENV.fetch("INGEST_URL", "http://localhost:4000"))
    random = Random.new(Integer(ENV.fetch("SEED", Random.new_seed.to_s)))

    dummy = DummyLogs.new(random: random, now: Time.now.utc, hours: hours)
    envelope_logs = dummy.take([ count / 50, 500 ].min, styled: false)
    logs = dummy.take(count - envelope_logs.size)

    puts "Sending #{logs.size + envelope_logs.size} Logs over the last #{hours} h to Project #{project.slug} " \
      "(id #{project.id}) at #{ingest}"
    totals = Hash.new(0)
    logs.each_slice(1000) do |batch|
      body = batch.map { |log| JSON.generate(log) }.join("\n")
      answer = DummyLogs.post(ingest, "/api/#{project.id}/logs", body,
        "Content-Type" => "application/x-ndjson", "Authorization" => "Bearer #{key}")
      answer.each { |name, n| totals[name] += n }
      print "."
    end
    envelope_logs.each_slice(100) do |batch|
      DummyLogs.post(ingest, "/api/#{project.id}/envelope/", DummyLogs.envelope(batch),
        "Content-Type" => "application/x-sentry-envelope",
        "X-Sentry-Auth" => "Sentry sentry_version=7, sentry_key=#{key}")
      totals["envelope"] += batch.size
      print "."
    end
    puts
    puts "JSON route: #{totals["accepted"]} accepted, #{totals["dropped"]} dropped, #{totals["truncated"]} truncated; " \
      "#{totals["envelope"]} sent in Sentry envelopes"
    puts "Open the Logs explorer for #{project.slug} and pick a range covering the last #{hours} h."
  end
end

# Builds the dummy Logs. Each "request" yields a few Logs sharing a trace and
# request id; an incident a few hours back adds a burst of errors.
class DummyLogs
  SERVICES = %w[api worker billing web].freeze
  ENVIRONMENTS = [ "production", "production", "production", "staging" ].freeze
  REGIONS = %w[eu-west-1 us-east-1 ap-south-1].freeze
  PATHS = [ "/api/v1/orders", "/api/v1/orders/:id", "/api/v1/users/:id", "/api/v1/search", "/api/v1/cart",
    "/healthz", "/api/v1/invoices" ].freeze
  JOBS = %w[SendInvoiceJob SyncInventoryJob ReindexSearchJob ExpireCartsJob DeliverWebhookJob].freeze
  TAGS = %w[checkout mobile beta internal canary].freeze

  def self.post(base, path, body, headers)
    uri = URI.join(base, path)
    response = Net::HTTP.post(uri, body, headers)
    abort "\n#{uri} answered #{response.code}: #{response.body}" unless response.is_a?(Net::HTTPSuccess)
    # The JSON route answers counts; the envelope route answers a bare "OK".
    counts = JSON.parse(response.body) rescue {}
    counts.is_a?(Hash) ? counts.select { |_, value| value.is_a?(Integer) } : {}
  rescue Errno::ECONNREFUSED
    abort "\nNothing is listening at #{base}. Is EventStore running (bin/dev)?"
  end

  # A Sentry envelope with one `log` item, in the shape current SDKs send.
  def self.envelope(logs)
    items = logs.map do |log|
      attributes = log.except("time", "level", "message", "service", "environment", "trace_id")
        .transform_values { |value| sentry_attribute(value) }
      attributes["service.name"] = { value: log["service"], type: "string" } if log["service"]
      attributes["sentry.environment"] = { value: log["environment"], type: "string" } if log["environment"]
      { timestamp: Time.iso8601(log["time"]).to_f, level: log["level"] || "info", body: log["message"],
        trace_id: log["trace_id"], attributes: attributes }.compact
    end
    payload = JSON.generate(items: items)
    header = { type: "log", item_count: items.size, content_type: "application/vnd.sentry.items.log+json" }
    [ "{}", JSON.generate(header), payload ].join("\n")
  end

  def self.sentry_attribute(value)
    case value
    when true, false then { value: value, type: "boolean" }
    when Integer then { value: value, type: "integer" }
    when Float then { value: value, type: "double" }
    else { value: value.is_a?(String) ? value : JSON.generate(value), type: "string" }
    end
  end

  def initialize(random:, now:, hours:)
    @random = random
    @now = now
    @from = now - hours * 3600
    @incident_at = now - [ hours * 3600 / 4, 3 * 3600 ].min
  end

  # Logs in senders' varied spellings, or (styled: false) always time/level/message.
  def take(count, styled: true)
    logs = []
    logs.concat(request_logs(styled)) while logs.size < count
    logs.first(count)
  end

  private

  attr_reader :random

  def request_logs(styled)
    at = random_time
    incident = (at - @incident_at).abs < 15 * 60
    service = SERVICES.sample(random: random)
    context = {
      "trace_id" => SecureRandom.hex(16), "request_id" => SecureRandom.uuid,
      "environment" => ENVIRONMENTS.sample(random: random), "region" => REGIONS.sample(random: random),
    }
    logs = send(:"#{service}_logs", context, incident)
    logs.each_with_index.map do |log, i|
      log = log.merge("service" => service)
      styled ? styled(log, at + i * 0.004) : log.merge("time" => (at + i * 0.004).iso8601(3))
    end
  end

  def api_logs(context, incident)
    method = %w[GET GET GET POST PUT DELETE].sample(random: random)
    path = PATHS.sample(random: random)
    status = incident && random.rand < 0.6 ? [ 500, 502, 503 ].sample(random: random) : weighted(200 => 85, 201 => 5, 404 => 6, 422 => 3, 500 => 1)
    duration = (incident ? random.rand(800.0..4000.0) : random.rand(4.0..250.0)).round(2)
    user = random.rand < 0.8 ? random.rand(1..5000) : nil
    level = if status >= 500 then "error" elsif status >= 400 then "warn" else "info" end
    logs = [
      context.merge("level" => "debug", "message" => "Started #{method} #{path}",
        "http" => { "method" => method, "path" => path }, "user.id" => user).compact,
      context.merge("level" => level, "message" => "#{method} #{path} #{status} in #{duration.round}ms",
        "http.method" => method, "http.path" => path, "http.status" => status, "duration_ms" => duration,
        "user.id" => user, "cache.hit" => random.rand < 0.4, "tags" => TAGS.sample(random.rand(0..2), random: random)).compact
    ]
    if status >= 500
      logs << context.merge("level" => "error", "message" => "Upstream timed out after #{duration.round}ms",
        "error" => { "type" => "Net::ReadTimeout", "upstream" => "inventory-service" })
    end
    logs
  end

  def worker_logs(context, incident)
    job = JOBS.sample(random: random)
    attempts = random.rand < 0.9 ? 1 : random.rand(2..5)
    failed = incident ? random.rand < 0.4 : random.rand < 0.03
    duration = random.rand(20.0..9000.0).round(1)
    [
      context.merge("level" => "info", "message" => "Performing #{job}", "job.name" => job,
        "job.queue" => %w[default mailers critical].sample(random: random), "job.attempts" => attempts),
      if failed
        context.merge("level" => attempts >= 5 ? "fatal" : "error", "message" => "#{job} failed: connection reset by peer",
          "job.name" => job, "job.attempts" => attempts, "error.type" => "Errno::ECONNRESET")
      else
        context.merge("level" => duration > 5000 ? "warn" : "info", "message" => "Performed #{job} in #{duration.round}ms",
          "job.name" => job, "duration_ms" => duration)
      end
    ]
  end

  def billing_logs(context, incident)
    amount = random.rand(5.0..900.0).round(2)
    currency = %w[USD EUR GBP INR].sample(random: random)
    declined = random.rand < (incident ? 0.3 : 0.05)
    customer = "cus_#{SecureRandom.alphanumeric(10, chars: [ *"a".."z", *"0".."9" ])}"
    [
      context.merge("level" => declined ? "warn" : "info",
        "message" => declined ? "Card declined for #{customer}" : "Charge succeeded",
        "payment" => { "amount" => amount, "currency" => currency, "provider" => "stripe" },
        "customer.id" => customer, "payment.declined" => declined,
        # A Stripe-style id too large for a DOUBLE, kept exact as a string.
        "charge.number" => 9_007_199_254_740_993 + random.rand(1000))
    ]
  end

  def web_logs(context, _incident)
    page = %w[dashboard orders settings invoices search].sample(random: random)
    render = random.rand(15.0..600.0).round(1)
    [
      context.merge("level" => render > 400 ? "warn" : "info", "message" => "Rendered #{page} in #{render.round}ms",
        "page" => page, "render_ms" => render, "browser" => %w[Chrome Firefox Safari Edge].sample(random: random),
        "viewport" => { "width" => [ 390, 768, 1280, 1920 ].sample(random: random), "height" => [ 844, 1024, 800, 1080 ].sample(random: random) })
    ]
  end

  # Spells some Logs the way other loggers do: pino/zap-ish keys, epoch times.
  def styled(log, at)
    case random.rand
    when 0...0.7
      log.merge("time" => at.iso8601(3))
    when 0.7...0.85
      log.except("message", "level").merge("ts" => (at.to_f * 1000).round, "msg" => log["message"],
        "severity" => log["level"]&.then { |level| { "warn" => "warning", "error" => "err", "fatal" => "critical" }.fetch(level, level) })
    else
      log.except("message").merge("@timestamp" => at.iso8601(6), "log" => log["message"])
    end.compact
  end

  def random_time
    # Busier in the last few hours, with a burst around the incident.
    offset = random.rand < 0.08 ? (@incident_at - @from) + random.rand(-900.0..900.0) : (@now - @from) * (random.rand**0.6)
    [ [ @from + offset, @from ].max, @now ].min
  end

  def weighted(weights)
    pick = random.rand(weights.values.sum)
    weights.each { |value, weight| return value if (pick -= weight).negative? }
  end
end
