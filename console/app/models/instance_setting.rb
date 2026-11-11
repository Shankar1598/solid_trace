# frozen_string_literal: true

# The install's settings: one record. Instance admins set email (the SMTP
# server and the sender) on the Email settings page or over the MCP server,
# and the settings that apply at once (EDITABLE: SQL queries, the default
# Retention of Logs and of traces, log-all-reads, log-MCP-reads, the
# text-match budget, the Log read threads, the hours on local disk, Console's
# and ingest's URLs) on Settings → Instance or over the MCP server.
# Sign-up has no UI on purpose: opening it lets anyone who can reach Console
# make an account, so it changes only from a shell on the server, with
# `bin/rails instance_settings:set` (lib/tasks/instance_settings.rake), and a
# stolen instance admin session or API token can't open the install. Until
# something is first set there's no row and the column defaults apply.
#
# EventStore reads this table read-only, and assumes the same defaults when the
# row is missing (event_store/storage/console_settings.go).
class InstanceSetting < ApplicationRecord
  ID = 1

  # The settings that apply at once, so instance admins change them on
  # Settings → Instance and over the MCP server.
  EDITABLE = %i[sql_queries_enabled default_log_retention_days default_trace_retention_days log_all_reads
                log_mcp_reads send_console_issues trace_console logs_text_match_budget_mb logs_read_threads
                local_cache_hours console_url ingest_url].freeze

  # What turning SQL queries on allows (docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md).
  SQL_QUERIES_DESCRIPTION = <<~TEXT
    SQL queries let every member of every Organization run ClickHouse SQL from the Logs explorer and the MCP server.
    Each runs as its Project's read-only database user, which sees only that Project's logs, events and spans tables.
    The database refuses the rest: other Projects, other tables, system tables, files, URLs, the bucket and any change.
    A query stops after 10 seconds or 1 GiB of memory, and can't raise its limits.
    So turning them on lets any member:
      - read every Log, Event and Span of their own Organization's Projects
      - slow ingest down for up to 10 seconds a query
    They are on by default. Turn them off if you don't trust every member of every Organization not to slow the install down.
  TEXT

  # The Meta Organization (ADR 0011), until it is deleted.
  belongs_to :meta_organization, class_name: "Organization", optional: true

  # Encrypted with keys derived from SECRET_KEY_BASE (ADR 0015).
  encrypts :smtp_password

  validates :sql_queries_enabled, inclusion: { in: [ true, false ] }
  validates :sign_up_open, inclusion: { in: [ true, false ] }
  validates :log_all_reads, inclusion: { in: [ true, false ] }
  # Off still saves while log_all_reads is on, and applies once that's off.
  validates :log_mcp_reads, inclusion: { in: [ true, false ] }
  # Whether Console sends its own Issues and traces to its Sentry DSN, the
  # Meta Project on an install (ConsoleSentry, docs/traces.md).
  validates :send_console_issues, inclusion: { in: [ true, false ] }
  validates :trace_console, inclusion: { in: [ true, false ] }
  validates :default_log_retention_days, numericality: { only_integer: true, greater_than: 0 }
  validates :default_trace_retention_days, numericality: { only_integer: true, greater_than: 0 }
  # The most a Query that matches text may read, in MiB of the kept bytes in
  # its range and the files its filters keep, before the Logs explorer and
  # the MCP server refuse it; 0 refuses none. EventStore reads it on each
  # Query it sizes (event_store/handler/logs_budget.go).
  validates :logs_text_match_budget_mb, numericality: { only_integer: true, greater_than_or_equal_to: 0 }
  # ClickHouse's max_threads for each tagged Log read; 0 sets none, so
  # ClickHouse's default of one per CPU core applies. EventStore reads it on
  # each request that reads Logs (event_store/handler/logs_budget.go), and
  # takes a value out of this range as 0 (storage.MaxLogReadThreads).
  validates :logs_read_threads, numericality: { only_integer: true, in: 0..64 }
  # How long Logs and Spans stay on local disk before their parts move to the
  # bucket, with one; 0 moves each part once stored. EventStore reads it at
  # start and every minute, and sets the day tables' TTL when it changed
  # (event_store/storage/local_cache_hours.go). Without a bucket it does
  # nothing.
  validates :local_cache_hours, numericality: { only_integer: true, in: 0..720 }

  # Console's URL and ingest's URL, as people, links and SDKs reach them, or
  # nil to fall back to CONSOLE_URL and PUBLIC_INGEST_URL, then the request's
  # address (PublicUrls, ADR 0017). Console's has no path; ingest's may.
  normalizes :console_url, :ingest_url, with: ->(value) { PublicUrls.clean(value) }
  validate -> { url_problem(:console_url, path: false) }, if: -> { console_url.present? }
  validate -> { url_problem(:ingest_url, path: true) }, if: -> { ingest_url.present? }
  # Saved from the page or MCP (update_editable), Console's URL must keep the
  # scheme of the HTTPS mode Console started in, so the dashboard can't lock
  # its admin out. The mode changes only with CONSOLE_URL in deploy/.env and a
  # restart; a shell's instance_settings:set saves any scheme, for recovery.
  validate :console_url_keeps_https_mode, on: :editable,
    if: -> { console_url.present? && will_save_change_to_console_url? && errors[:console_url].empty? }

  # What changing local_cache_hours does, for update_instance_settings'
  # description. Settings → Instance's confirmation says the same, with the
  # old and new hours (Settings/Instance.tsx).
  LOCAL_CACHE_HOURS_WARNING = "Logs older than this are read from the bucket, which is slower than local disk; " \
                              "lowering it moves more of the recent Logs there within a minute or two. Raising it " \
                              "keeps more on local disk, which counts toward the disk limit; parts already in the " \
                              "bucket stay there. ClickHouse recomputes every part's move time in the background, " \
                              "reading each part's time column from the bucket, so change it rarely."

  # Console's Sentry reads these settings once in a while; this process reads
  # them again at once.
  after_save { ConsoleSentry.reset }

  normalizes :smtp_address, :smtp_username, :mail_from, with: ->(value) { value.strip.presence }
  validates :smtp_port, numericality: { only_integer: true, in: 1..65_535 }
  validates :mail_from, presence: true, if: -> { smtp_address.present? }
  validate :mail_from_is_an_address, if: -> { mail_from.present? }
  # Without a username Console doesn't sign in, so a password would go unused.
  before_save -> { self.smtp_password = nil }, if: -> { smtp_username.blank? && smtp_password_saved? }

  def self.current
    find_by(id: ID) || new(id: ID)
  end

  # Whether someone may sign up: while the install has no user, so whoever set
  # it up can make the first account, or when the operator has opened sign-up.
  # Otherwise people join by Invite.
  def sign_up_allowed?
    sign_up_open? || !User.exists?
  end

  # Changes the settings that apply at once, as Settings → Instance and the MCP
  # server do: only those given change, and any other key is ignored. A
  # Console URL on another host strands the Passkeys made for the current one
  # (ADR 0018), so a change that would otherwise save is refused with their
  # counts, as the :passkeys_stop_working error on base, unless
  # confirm_passkeys_stop_working. A shell's instance_settings:set doesn't ask.
  def update_editable(changes, confirm_passkeys_stop_working: false)
    assign_attributes(changes.to_h.symbolize_keys.slice(*EDITABLE))
    return false unless valid?(:editable)

    stranded = PasskeyOffer.current(settings: InstanceSetting.current).stranded_by(PasskeyOffer.current(settings: self))
    if stranded && !confirm_passkeys_stop_working
      errors.add(:base, :passkeys_stop_working, message: stranded)
      return false
    end
    save(context: :editable)
  end

  # Changes the email settings as the Email settings page and the MCP server
  # do: only those given change, a blank password keeps the saved one, and
  # clear_password removes it.
  def update_email(changes, clear_password: false)
    changes = changes.to_h.symbolize_keys.slice(:smtp_address, :smtp_port, :smtp_username, :smtp_password, :mail_from)
    changes.delete(:smtp_password) if changes[:smtp_password].blank?
    changes[:smtp_password] = nil if clear_password
    update(changes)
  end

  # Why Console can't send email with these settings, or nil when it can.
  def email_problem
    if smtp_address.blank?
      "No SMTP host: Console sends no email"
    elsif mail_from.blank?
      "No sender: Console sends no email"
    elsif smtp_username.present? && !smtp_password_readable?
      if smtp_password_saved?
        "The SMTP password can't be read, as Console's secret changed: enter it again"
      else
        "No SMTP password for the username"
      end
    end
  end

  def email_set_up?
    email_problem.nil?
  end

  # Whether a password is saved, readable or not. It is never shown.
  def smtp_password_saved?
    smtp_password_before_type_cast.present?
  end

  # Whether the saved password decrypts: it doesn't once SECRET_KEY_BASE has
  # changed, and then it must be entered again.
  def smtp_password_readable?
    smtp_password.present?
  rescue ActiveRecord::Encryption::Errors::Decryption
    false
  end

  # Action Mailer's SMTP settings, or nil when email isn't set up. Port 465 is
  # implicit TLS; any other port uses STARTTLS when the server offers it. Short
  # timeouts make a wrong host fail within seconds.
  def smtp_options
    return unless email_set_up?

    options = { address: smtp_address, port: smtp_port, open_timeout: 10, read_timeout: 10 }
    options.merge!(user_name: smtp_username, password: smtp_password, authentication: :plain) if smtp_username.present?
    if smtp_port == 465
      options.merge!(tls: true)
    else
      options.merge!(enable_starttls_auto: true)
    end
  end

  private

  URL_FORMATS = {
    false => "must be http:// or https:// and a host, with a port if needed and no path, like https://console.example.com",
    true => "must be http:// or https:// and a host, with a port and a path if needed, like https://ingest.example.com:4443",
  }.freeze

  # attribute's URL is http(s)://host[:port], and [/path] when path is true:
  # no user, query or fragment.
  def url_problem(attribute, path:)
    uri = URI.parse(self[attribute])
    return if uri.is_a?(URI::HTTP) && uri.host.present? && uri.userinfo.nil? && uri.query.nil? && uri.fragment.nil? &&
      (path || uri.path.empty?)

    errors.add(attribute, URL_FORMATS[path])
  rescue URI::InvalidURIError
    errors.add(attribute, URL_FORMATS[path])
  end

  def console_url_keeps_https_mode
    https = PublicUrls.https?
    return if URI.parse(console_url).scheme == (https ? "https" : "http")

    errors.add(:console_url, "must start with #{https ? "https" : "http"}://, as Console started with#{"out" unless https} " \
                             "HTTPS. HTTPS changes only with CONSOLE_URL's scheme in deploy/.env and a restart")
  end

  # One address, with or without a name, as in SolidTrace <alerts@example.com>.
  def mail_from_is_an_address
    addresses = Mail::AddressList.new(mail_from).addresses
    return if addresses.one? && addresses.first.local.present? && addresses.first.domain.present?

    errors.add(:mail_from, "isn't an email address, like SolidTrace <alerts@example.com>")
  rescue Mail::Field::ParseError
    errors.add(:mail_from, "isn't an email address, like SolidTrace <alerts@example.com>")
  end
end
