# frozen_string_literal: true

# The install's settings: one record. Instance admins set email (the SMTP
# server and the sender) on the Email settings page or over the MCP server.
# The operator sets the others with `bin/rails instance_settings:set`
# (lib/tasks/instance_settings.rake) or `InstanceSetting.current.update!(...)`
# in a Rails console; they have no UI yet. Until something is first set there's
# no row and the column defaults apply.
#
# EventStore reads this table read-only, and assumes the same defaults when the
# row is missing (event_store/storage/console_settings.go).
class InstanceSetting < ApplicationRecord
  ID = 1

  # What turning SQL queries on allows (docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md).
  SQL_QUERIES_DESCRIPTION = <<~TEXT
    SQL queries let every member of every Organization run raw DuckDB SQL from the Logs explorer and the MCP server.
    They are read-only: anything but one SELECT is refused, so they can't change data, settings or files.
    They read only the logs and events views of the Project they run on: tables, files, URLs and the catalogue are refused.
    But they are not sandboxed, so turning them on lets any member:
      - read every Log and Event of their own Organization's Projects
      - starve ingest with slow queries
    They are on by default. Turn them off if you don't trust every member of every Organization not to slow the install down.
  TEXT

  # The Meta Organization (ADR 0011), until it is deleted.
  belongs_to :meta_organization, class_name: "Organization", optional: true

  # Encrypted with keys derived from SECRET_KEY_BASE (ADR 0015).
  encrypts :smtp_password

  validates :sql_queries_enabled, inclusion: { in: [ true, false ] }
  validates :sign_up_open, inclusion: { in: [ true, false ] }
  validates :log_all_reads, inclusion: { in: [ true, false ] }
  validates :default_log_retention_days, numericality: { only_integer: true, greater_than: 0 }

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

  # One address, with or without a name, as in SolidTrace <alerts@example.com>.
  def mail_from_is_an_address
    addresses = Mail::AddressList.new(mail_from).addresses
    return if addresses.one? && addresses.first.local.present? && addresses.first.domain.present?

    errors.add(:mail_from, "isn't an email address, like SolidTrace <alerts@example.com>")
  rescue Mail::Field::ParseError
    errors.add(:mail_from, "isn't an email address, like SolidTrace <alerts@example.com>")
  end
end
