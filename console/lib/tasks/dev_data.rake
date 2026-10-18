# frozen_string_literal: true

require "json"
require "net/http"
require "securerandom"

# Dummy data for every Console page in development, so each page has
# something to show: a second Project, Issues with Events (sent through
# EventStore's ingest, as an SDK would), comments and an assignee,
# Integrations, a Dashboard and Monitors. Safe to rerun: what exists is kept.
#
#   cd console && mise exec -- bin/rails db:seed dev:data
namespace :dev do
  desc "Fill the development database with dummy Issues, Integrations, a Dashboard and Monitors. " \
    "INGEST_URL=http://localhost:4000, LOGS=0 skips sending fresh Logs"
  task data: :environment do
    abort "dev:data is for development only" if Rails.env.production?

    DevData.new(URI(ENV.fetch("INGEST_URL", "http://localhost:4000"))).run(logs: ENV["LOGS"] != "0")
  end
end

class DevData
  def initialize(ingest)
    @ingest = ingest
    @org = Organization.find_by!(slug: "solid-trace-org")
    @project = @org.projects.find_by!(slug: "solid-trace-project")
    @admin = @org.organization_users.joins(:user).find_by!(users: { email: "admin@solidtrace.local" })
    @member = @org.organization_users.joins(:user).find_by!(users: { email: "member@solidtrace.local" })
  end

  def run(logs:)
    worker = @org.projects.find_or_create_by!(slug: "billing-worker") { |p| p.name = "Billing Worker" }
    worker.project_keys.find_or_create_by!(public_key: "billingkey123") { |k| k.secret_key = "billingsecret123" }

    send_issues(@project, WEB_ISSUES)
    send_issues(worker, WORKER_ISSUES)
    triage
    # Monitors read the last hour or so, which older dummy Logs miss.
    Rake::Task["dev:logs"].tap { ENV["COUNT"] = "3000"; ENV["HOURS"] = "2" }.invoke if logs
    integrations = create_integrations
    create_dashboard
    create_monitors(integrations)
    puts "Done: #{Issue.count} Issues, #{Integration.count} Integrations, #{Dashboard.count} Dashboards, " \
      "#{LogMonitor.count} Monitors"
  end

  private

  # Sends each scenario's Events, unless the Project already has Issues,
  # then waits for EventStore to turn them into Issues.
  def send_issues(project, scenarios)
    if project.issues.exists?
      puts "#{project.slug} has Issues; sending no Events"
      return
    end
    key = project.project_keys.first!.public_key
    sent = 0
    scenarios.each_with_index do |scenario, i|
      scenario[:events].times do |n|
        at = Time.now.utc - (scenario[:events] - n) * (3 + i) * 3600 + rand(0..600)
        post(project, key, event(scenario, at, n))
        sent += 1
      end
    end
    puts "Sent #{sent} Events to #{project.slug}"
    30.times do
      break if project.issues.count >= scenarios.size
      sleep 1
    end
    puts "#{project.slug} has #{project.issues.count} Issues"
  end

  def post(project, key, payload)
    uri = URI.join(@ingest, "/api/#{project.id}/store?sentry_key=#{key}")
    response = Net::HTTP.post(uri, JSON.generate(payload), "Content-Type" => "application/json")
    abort "#{uri} answered #{response.code}: #{response.body}" unless response.is_a?(Net::HTTPSuccess)
  rescue Errno::ECONNREFUSED
    abort "Nothing is listening at #{@ingest}. Is EventStore running (bin/dev)?"
  end

  def event(scenario, at, n)
    user = USERS[n % USERS.size]
    payload = {
      event_id: SecureRandom.hex(16), timestamp: at.iso8601(3), platform: scenario[:platform] || "ruby",
      level: scenario[:level], logger: scenario[:logger], transaction: scenario[:transaction],
      server_name: "web-#{(n % 3) + 1}", release: "2026.10.#{10 + (n % 5)}", environment: n.even? ? "production" : "staging",
      tags: { "region" => %w[eu-west-1 us-east-1][n % 2], "browser" => %w[Chrome Firefox Safari][n % 3] }
        .merge(scenario[:tags] || {}),
      user: user,
      request: { method: scenario[:method] || "GET", url: "https://shop.example.com#{scenario[:transaction]}",
        headers: { "User-Agent" => "Mozilla/5.0" } },
      contexts: { runtime: { name: "ruby", version: "3.4.1" }, os: { name: "Linux" } },
      breadcrumbs: { values: breadcrumbs(scenario, at) },
    }
    if scenario[:type]
      payload[:exception] = { values: [ { type: scenario[:type], value: scenario[:value],
        stacktrace: { frames: frames(scenario[:frames]) } } ] }
    else
      payload[:message] = scenario[:value]
    end
    payload.compact
  end

  def breadcrumbs(scenario, at)
    [
      { timestamp: (at - 4).to_f, category: "http", level: "info", type: "http",
        data: { method: scenario[:method] || "GET", url: scenario[:transaction], status_code: 200 } },
      { timestamp: (at - 3).to_f, category: "sql.active_record", level: "info",
        message: "SELECT \"users\".* FROM \"users\" WHERE \"users\".\"id\" = ? LIMIT 1" },
      { timestamp: (at - 1).to_f, category: "cache", level: "debug", message: "Read cart/#{rand(1000..9999)}" },
      { timestamp: (at - 0.2).to_f, category: "log", level: scenario[:level] == "error" ? "error" : "warning",
        message: scenario[:value] }
    ]
  end

  # Frames oldest first, as Sentry sends them; the last is where it raised.
  def frames(app_frames)
    gem = [ { filename: "gems/actionpack-8.1.2/lib/action_controller/metal/basic_implicit_render.rb",
              function: "send_action", lineno: 8, in_app: false },
            { filename: "gems/actionpack-8.1.2/lib/abstract_controller/base.rb",
              function: "process_action", lineno: 226, in_app: false } ]
    gem + (app_frames || []).map do |file, function, line, code|
      { filename: file, abs_path: "/srv/shop/#{file}", function: function, lineno: line, in_app: true,
        pre_context: [ "  def #{function}", "    record = load_record" ], context_line: "    #{code}",
        post_context: [ "  end", "" ] }
    end
  end

  # Resolves two Issues, assigns one and comments on it.
  def triage
    issues = @project.issues.order(:number).to_a
    return if issues.empty?

    issues.last(2).each { |issue| issue.update!(status: Issue::STATUS::RESOLVED) } if issues.size > 3 && !Issue.where(status: Issue::STATUS::RESOLVED).exists?
    first = issues.first
    first.update!(assignee: @member) unless first.assignee
    [
      [ @admin, "Seeing this after the 2026.10.12 deploy. Looks like the cart is nil when the session expires mid-checkout." ],
      [ @member, "Reproduced locally. I'll guard the nil cart and add a test; fix should go out with the next release." ]
    ].each { |who, text| first.comments.create!(organization_user: who, content: text) } unless first.comments.exists?
  end

  def create_integrations
    [
      [ "slack", "#alerts-checkout", { "webhook_url" => "https://hooks.slack.example.com/services/T000/B000/XXXXXXXX",
        "notify_on_new_issue" => false } ],
      [ "pagerduty", "On-call (payments)", { "routing_key" => "dummy0000000000000000000000000000", "severity" => "critical",
        "notify_on_new_issue" => false } ],
      [ "email", "Engineering team", { "recipients" => "eng@example.com, oncall@example.com",
        "notify_on_new_issue" => false } ]
    ].map do |provider, name, settings|
      @org.integrations.find_or_create_by!(provider: provider, name: name) { |i| i.settings = settings; i.active = true }
    end
  end

  def create_dashboard
    source = "['#{@project.slug}']"
    dashboard = @org.dashboards.find_or_create_by!(name: "Checkout service health") do |d|
      d.description = "Traffic, errors and latency of the storefront and its jobs."
      d.default_range = "24h"
      d.created_by = @admin.user
    end
    return if dashboard.charts.exists?

    [
      [ "Logs by level", "bar", 0, 0, 8, 4, "#{source}\n| summarize count() by bin_auto(time), level", { stacked: true } ],
      [ "Errors", "stat", 8, 0, 4, 4, "#{source}\n| where level in (\"error\", \"fatal\")\n| summarize count()", { unit: "errors" } ],
      [ "Average latency by service", "line", 0, 4, 6, 4,
        "#{source}\n| where duration_ms > 0\n| summarize avg(duration_ms) by bin_auto(time), service", { unit: "ms", decimals: 0 } ],
      [ "Requests by region", "area", 6, 4, 6, 4,
        "#{source}\n| where isnotnull(region)\n| summarize count() by bin_auto(time), region", { stacked: true } ],
      [ "Slowest services", "table", 0, 8, 12, 4,
        "#{source}\n| where duration_ms > 0\n| summarize count(), avg(duration_ms), max(duration_ms) by service", {} ]
    ].each do |title, viz, x, y, w, h, query, options|
      dashboard.charts.create!(title: title, viz: viz, x: x, y: y, w: w, h: h, query: query, options: options, project: @project)
    end
  end

  def create_monitors(integrations)
    source = "['#{@project.slug}']"
    [
      { name: "Server errors on checkout", kind: "threshold", operator: ">", threshold: 5,
        query: "#{source}\n| where level == \"error\"\n| summarize count()", interval_minutes: 5, range_minutes: 60 },
      { name: "Slow requests", kind: "threshold", operator: ">", threshold: 5000,
        query: "#{source}\n| where duration_ms > 0\n| summarize avg(duration_ms) by service", interval_minutes: 5, range_minutes: 60 },
      { name: "Fatal job failures", kind: "match", query: "#{source}\n| where level == \"fatal\"", interval_minutes: 1, range_minutes: 5 }
    ].each_with_index do |attrs, i|
      monitor = @org.monitors.find_or_initialize_by(name: attrs[:name])
      next if monitor.persisted?

      monitor.assign_attributes(attrs.merge(project: @project))
      monitor.integrations = [ integrations[i % integrations.size] ]
      monitor.save!
    end
  end

  USERS = [
    { id: "4211", email: "maya@example.com", username: "maya", ip_address: "203.0.113.24" },
    { id: "5873", email: "jonas@example.com", username: "jonas", ip_address: "198.51.100.7" },
    { id: "6120", email: "priya@example.com", username: "priya", ip_address: "192.0.2.41" }
  ].freeze

  WEB_ISSUES = [
    { type: "NoMethodError", value: "undefined method 'total' for nil", level: "error", events: 6,
      transaction: "/checkout", method: "POST", tags: { "feature" => "checkout" },
      frames: [ [ "app/controllers/checkouts_controller.rb", "create", 42, "total = @cart.total" ],
                [ "app/services/checkout_total.rb", "call", 17, "cart.total + shipping" ] ] },
    { type: "ActiveRecord::RecordNotFound", value: "Couldn't find Order with 'id'=99812", level: "error", events: 4,
      transaction: "/orders/99812", frames: [ [ "app/controllers/orders_controller.rb", "show", 12, "@order = Order.find(params[:id])" ] ] },
    { type: "Net::ReadTimeout", value: "Net::ReadTimeout with #<TCPSocket:(closed)>", level: "error", events: 5,
      transaction: "/products/search", tags: { "upstream" => "inventory-service" },
      frames: [ [ "app/clients/inventory_client.rb", "stock_for", 58, "http.request(request)" ],
                [ "app/controllers/products_controller.rb", "search", 23, "InventoryClient.stock_for(ids)" ] ] },
    { type: nil, value: "Payment provider answered slowly (2.4 s)", level: "warning", events: 3,
      transaction: "/payments", method: "POST", logger: "payments" },
    { type: "JSON::ParserError", value: "unexpected token at '<html>'", level: "error", events: 2,
      transaction: "/webhooks/stripe", method: "POST",
      frames: [ [ "app/controllers/webhooks/stripe_controller.rb", "create", 9, "JSON.parse(request.body.read)" ] ] },
    { type: "ActionController::InvalidAuthenticityToken", value: "Can't verify CSRF token authenticity.", level: "warning",
      events: 2, transaction: "/account", method: "PATCH",
      frames: [ [ "app/controllers/accounts_controller.rb", "update", 31, "verify_authenticity_token" ] ] }
  ].freeze

  WORKER_ISSUES = [
    { type: "Errno::ECONNRESET", value: "Connection reset by peer", level: "fatal", events: 4,
      transaction: "InvoiceMailerJob", tags: { "queue" => "mailers" },
      frames: [ [ "app/jobs/invoice_mailer_job.rb", "perform", 14, "InvoiceMailer.with(invoice:).deliver_now" ] ] },
    { type: "Stripe::CardError", value: "Your card was declined.", level: "error", events: 3,
      transaction: "ChargeSubscriptionJob", tags: { "queue" => "critical" },
      frames: [ [ "app/jobs/charge_subscription_job.rb", "perform", 27, "Stripe::Charge.create(params)" ] ] }
  ].freeze
end
