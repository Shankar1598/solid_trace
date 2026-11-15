# frozen_string_literal: true

require "net/http"
require "json"

# Console's calls to the Dispatcher's private API, at the Dispatcher URL of
# Settings → Instance, as the signed-in user. Each call sends a new
# Console's factory token (Oauth::ConsoleToken) for that user in one
# Organization, so the Dispatcher checks the user and their role there as
# it does for each person.
#
#   Dispatcher.new(user: current_user, organization: @current_org).request(:get, "/api/v1/projects")
#
# A Dispatcher that does not answer, or answers with what is not JSON,
# raises Unavailable; an answer with an error status raises Refused. Each
# message says what went wrong, for the page to show.
class Dispatcher
  # The Dispatcher did not answer: no URL is set, the connection failed or
  # timed out, or the answer was not JSON.
  class Unavailable < StandardError; end

  # The Dispatcher answered with an error status. message is its error text.
  class Refused < StandardError
    attr_reader :status, :body

    def initialize(status, body)
      @status = status
      @body = body
      error = body.is_a?(Hash) ? body["error"].presence : nil
      super(error || "The Dispatcher refused the request (HTTP #{status}).")
    end
  end

  OPEN_TIMEOUT = 3
  READ_TIMEOUT = 10
  STATUS_PATH = "/api/v1/status"

  # Whether the Dispatcher URL is set. Without it, Console shows no part of
  # the Factory that needs the Dispatcher.
  def self.on?(settings: InstanceSetting.current)
    settings.dispatcher_url.present?
  end

  def initialize(user:, organization:, settings: InstanceSetting.current)
    @user = user
    @organization = organization
    @url = settings.dispatcher_url
  end

  # The answer to method on path (with body as JSON, if given), parsed.
  def request(method, path, body: nil)
    status, parsed = call(method, path, body)
    raise Refused.new(status, parsed) unless status.between?(200, 299)

    parsed
  end

  # The Dispatcher's status: { "version", "solidtrace" => { "url",
  # "token_active", "error" } }. It answers 503 when SolidTrace refuses the
  # Dispatcher's own token, with the same body.
  def status
    code, parsed = call(:get, STATUS_PATH, nil)
    return parsed if [ 200, 503 ].include?(code) && parsed.is_a?(Hash) && parsed.key?("solidtrace")

    raise Refused.new(code, parsed)
  end

  private

  def call(method, path, body)
    raise Unavailable, "No Dispatcher URL is set in Settings → Instance." if @url.blank?

    uri = URI.join("#{@url.chomp("/")}/", path.delete_prefix("/"))
    request = Net::HTTP.const_get(method.to_s.capitalize).new(uri)
    request["Authorization"] = "Bearer #{Oauth::ConsoleToken.issue(user: @user, organization: @organization)}"
    request["Accept"] = "application/json"
    if body
      request["Content-Type"] = "application/json"
      request.body = body.to_json
    end
    response = Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https",
                               open_timeout: OPEN_TIMEOUT, read_timeout: READ_TIMEOUT) do |http|
      http.request(request)
    end
    [ response.code.to_i, parse(response) ]
  rescue Net::OpenTimeout, Net::ReadTimeout
    raise Unavailable, "The Dispatcher at #{@url} did not answer in time. Check that it runs and that Console " \
                       "can reach it at this URL."
  rescue SocketError, SystemCallError, IOError, OpenSSL::SSL::SSLError, EOFError => e
    raise Unavailable, "Console could not connect to the Dispatcher at #{@url} (#{e.message}). Check that it " \
                       "runs and that Console can reach it at this URL."
  end

  def parse(response)
    return {} if response.body.blank? && response.code.to_i == 204

    JSON.parse(response.body.to_s)
  rescue JSON::ParserError
    raise Unavailable, "The Dispatcher at #{@url} answered HTTP #{response.code} with no JSON. Check that the " \
                       "Dispatcher URL is the Dispatcher's private API."
  end
end
