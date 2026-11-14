# frozen_string_literal: true

require "ipaddr"
require "net/http"

module Oauth
  # A client's published description: a Client ID Metadata Document, whose
  # HTTPS URL is the client's client_id (as Claude Code uses). Console
  # knows the client from this address, so the approval page says the name
  # is checked: the client must own the host that publishes it.
  #
  # A URL from a stranger is only given the form check (.well_formed?).
  # Console fetches the description (.client) only for a signed-in user,
  # and keeps the copy on the client row until its HTTP cache time ends (at
  # least 5 minutes, at most 24 hours). The token endpoint and the other
  # JSON endpoints never fetch: they read the copy.
  #
  # The fetch guards against SSRF: it resolves the host first, refuses a
  # loopback, private, link-local, multicast or unspecified address, and
  # connects to the checked address, with no proxy. HTTPS only, no
  # redirects, 5 seconds for the full fetch, at most 64 KB of body and 80
  # KB from the socket in all, a JSON object only.
  module PublishedDescription
    MAX_URL = 512
    MAX_BYTES = 64.kilobytes
    # The bytes that the status line, the headers and the chunk sizes may add
    # to the body.
    HEADER_BYTES = 16.kilobytes
    TIMEOUT = 5
    MIN_FRESH = 5.minutes
    MAX_FRESH = 24.hours
    # A fetch that started this long ago has stopped, so another can start.
    FETCH_LASTS = 30.seconds
    # Fetches for each user.
    LIMIT = 10
    LIMIT_WITHIN = 3.minutes
    # Addresses that IPAddr does not name but that reach this computer or
    # its network: the unspecified ones (0.0.0.0 reaches loopback on Linux)
    # and multicast.
    REFUSED_RANGES = %w[0.0.0.0/8 ::/128 224.0.0.0/4 255.255.255.255/32 ff00::/8].map { |range| IPAddr.new(range) }.freeze

    BUSY = "Console is checking this client now. Try again in a moment."
    TOO_MANY = "Console checked too many clients for you. Try again in a few minutes."

    # A problem with the description or its fetch, said in words for the
    # error page.
    class Problem < StandardError; end

    # A Problem that passes: another fetch runs, the user fetched too much,
    # or the host did not answer (a timeout, no connection, a TLS error, a
    # failed address lookup or an HTTP 5xx). It says nothing bad about the
    # description, so the device page does not deny the device code for it.
    class TryAgain < Problem; end

    # Raised when a connection reads more than MAX_BYTES + HEADER_BYTES.
    class TooMuchRead < StandardError; end

    # Counts the bytes that a Net::BufferedIO reads from the socket, and
    # stops at MAX_BYTES + HEADER_BYTES. Net::HTTP reads the status line and
    # each header line until a newline, with no limit on their size.
    module ReadCap
      def rbuf_fill
        before = rbuf_size
        super
        @bytes_read = @bytes_read.to_i + rbuf_size - before
        raise TooMuchRead if @bytes_read > MAX_BYTES + HEADER_BYTES
      end
    end

    # Gives a Net::HTTP's socket the ReadCap.
    module CappedConnection
      private

      def connect
        super
        @socket.extend(ReadCap)
      end
    end

    # The host's addresses, as strings. Tests replace it.
    mattr_accessor :resolver, default: ->(host) {
      Addrinfo.getaddrinfo(host, 443, nil, :STREAM, timeout: TIMEOUT).map(&:ip_address).uniq
    }
    # The store that counts each user's fetches; nil is Rails.cache.
    mattr_accessor :limit_store
    # The seconds that one fetch may take, from the connection to the last
    # byte. Tests make it shorter.
    mattr_accessor :time_limit, default: TIMEOUT

    module_function

    # Whether client_id claims to be a published description's URL, so it
    # gets the form check and not a look-up of a registered client.
    def url?(client_id)
      client_id.to_s.match?(%r{\Ahttps?://}i)
    end

    # The form check: https, a host name or a public address, a path, no
    # user info, no fragment, no dot segments, at most 512 characters.
    def well_formed?(url)
      return false unless url.is_a?(String) && url.length <= MAX_URL

      uri = URI(url)
      return false unless uri.scheme == "https" && uri.host.present? && uri.userinfo.nil? && uri.fragment.nil?
      return false if uri.path.blank? || uri.path == "/" || uri.path.split("/").intersect?(%w[. ..])

      address = ip_literal(uri.hostname)
      address.nil? || public_address?(address)
    rescue URI::InvalidURIError
      false
    end

    # Whether address (an IPAddr) is one that Console may connect to.
    def public_address?(address)
      address = address.native
      !(address.loopback? || address.private? || address.link_local? ||
        REFUSED_RANGES.any? { |range| range.family == address.family && range.include?(address) })
    end

    # The client for url with a fresh copy of its description, fetched for
    # user if the kept copy is old or missing. Only one fetch at a time runs
    # for one client_id: another request in that time gets the old copy, or
    # Problem (BUSY) when there is none. Raises Problem when the fetch or the
    # description fails.
    def client(url, user:)
      client = Client.find_by(client_id: url) || placeholder(url)
      return client if client.fresh?

      unless client.start_fetch!
        return client if client.fetched?

        raise TryAgain, BUSY
      end

      begin
        raise TryAgain, TOO_MANY if over_limit?(user)

        document, lasts = fetch(url)
        client.keep_description!(document, fresh_for: lasts)
      ensure
        client.end_fetch!
      end
      client
    end

    # The client for url, as .client gives it, whose description lists the
    # device grant: for the device page. Raises Problem when it does not.
    def device_client(url, user:)
      client = client(url, user: user)
      return client if client.grant_types.include?(DeviceCode::GRANT_TYPE)

      raise Problem, "The description at #{client.published_host} does not list the device grant " \
                     "(#{DeviceCode::GRANT_TYPE}) in its grant_types, so the program cannot sign in with a code."
    end

    # The description at url, checked, and how long it stays fresh.
    def fetch(url)
      uri = URI(url)
      address = checked_address(uri.hostname)
      response, body = get(uri, address)
      document = parse(body, uri)
      check(document, url, uri)
      [ document, fresh_for(response["Cache-Control"]) ]
    end

    # A connection to uri's host at address, the checked address, with no
    # proxy, so DNS is not asked again and cannot give another address.
    def connection(uri, address)
      Net::HTTP.new(uri.hostname, uri.port, nil).tap do |http|
        http.ipaddr = address
        http.use_ssl = true
        http.verify_mode = OpenSSL::SSL::VERIFY_PEER
        http.open_timeout = http.read_timeout = http.write_timeout = http.ssl_timeout = TIMEOUT
        http.max_retries = 0
      end
    end

    # A row for url, with no copy yet, so the fetch can lock it. Only a
    # signed-in user's request makes one.
    def placeholder(url)
      Client.create!(kind: "cimd", client_id: url, name: URI(url).host, redirect_uris: [], grant_types: [])
    rescue ActiveRecord::RecordNotUnique
      Client.find_by!(client_id: url)
    end

    def over_limit?(user)
      count = (limit_store || Rails.cache).increment("oauth/published-description-fetches/#{user.id}", 1,
                                                     expires_in: LIMIT_WITHIN)
      count.to_i > LIMIT
    end

    def checked_address(host)
      addresses = ip_literal(host) ? [ host ] : resolver.call(host)
      if addresses.blank?
        raise Problem, "Console could not find the address of #{host}, which publishes the client's description."
      end
      unless addresses.all? { |address| public_address?(IPAddr.new(address)) }
        raise Problem, "#{host} has a private or local address, so Console does not fetch the client's description."
      end

      addresses.first
    rescue SocketError
      raise TryAgain, "Console could not find the address of #{host}, which publishes the client's description. " \
                      "Try again later."
    rescue IPAddr::InvalidAddressError
      raise Problem, "Console could not find the address of #{host}, which publishes the client's description."
    end

    # Two guards stop a host that sends without end. The time limit covers
    # the full fetch (read_timeout starts again on each read), so a slow host
    # cannot hold a Puma thread. ReadCap limits all the bytes read from the
    # socket, so a long status line or long headers cannot fill memory; the
    # 64 KB check on the body cannot do this.
    def get(uri, address)
      http = connection(uri, address).extend(CappedConnection)
      body = +""
      response = nil
      Timeout.timeout(time_limit) do
        http.start do
          request = Net::HTTP::Get.new(uri, "Accept" => "application/json", "User-Agent" => "SolidTrace")
          http.request(request) do |answer|
            response = answer
            check_status(answer, uri)
            raise Problem, too_large(uri) if answer.content_length.to_i > MAX_BYTES

            answer.read_body do |chunk|
              body << chunk
              raise Problem, too_large(uri) if body.bytesize > MAX_BYTES
            end
          end
        end
      end
      [ response, body ]
    rescue TooMuchRead
      raise Problem, too_large(uri)
    rescue Net::OpenTimeout, Net::ReadTimeout, Net::WriteTimeout, Timeout::Error
      raise TryAgain, too_slow(uri)
    rescue SocketError, SystemCallError, IOError, OpenSSL::SSL::SSLError
      raise TryAgain, "Console could not fetch the client's description from #{uri.host}. Try again later."
    rescue Net::HTTPBadResponse, Net::ProtocolError, Net::HTTPHeaderSyntaxError, Zlib::Error
      raise Problem, "Console could not fetch the client's description from #{uri.host}: its answer is not valid HTTP."
    end

    def check_status(answer, uri)
      return if answer.is_a?(Net::HTTPOK)

      if answer.is_a?(Net::HTTPRedirection)
        raise Problem, "#{uri.host} answered the client's description with a redirect. Console follows no redirects."
      end

      if answer.is_a?(Net::HTTPServerError)
        raise TryAgain, "#{uri.host} answered the client's description with HTTP #{answer.code}. Try again later."
      end

      raise Problem, "#{uri.host} answered the client's description with HTTP #{answer.code}."
    end

    def parse(body, uri)
      document = JSON.parse(body)
      return document if document.is_a?(Hash)

      raise Problem, not_json(uri)
    rescue JSON::ParserError, EncodingError
      raise Problem, not_json(uri)
    end

    def check(document, url, uri)
      unless document["client_id"] == url
        raise Problem, "The description at #{uri.host} names another client_id, so Console does not use it."
      end

      redirect_uris = document["redirect_uris"]
      unless device_only?(document) ||
             (redirect_uris.is_a?(Array) && redirect_uris.any? { |redirect| Client.allowed_redirect_uri?(redirect) })
        raise Problem, "The description at #{uri.host} gives no https:// or loopback redirect_uris."
      end

      unless document["token_endpoint_auth_method"].nil? || document["token_endpoint_auth_method"] == "none"
        raise Problem, "The description at #{uri.host} is for a confidential client. Console accepts only " \
                       "token_endpoint_auth_method none, with PKCE."
      end
    end

    # Whether the description is for the device flow and not for the
    # approval page: its grant_types list the device grant and not
    # authorization_code, so it needs no redirect URI.
    def device_only?(document)
      grant_types = document["grant_types"]
      grant_types.is_a?(Array) && grant_types.include?(DeviceCode::GRANT_TYPE) && !grant_types.include?("authorization_code")
    end

    # The HTTP cache time from Cache-Control's max-age, held between 5
    # minutes and 24 hours; no max-age (or no-store, no-cache) is 5 minutes.
    def fresh_for(cache_control)
      directives = cache_control.to_s.downcase.split(",").map(&:strip)
      max_age = directives.filter_map { |directive| directive[/\Amax-age=(\d+)\z/, 1] }.first
      return MIN_FRESH if max_age.nil? || directives.intersect?(%w[no-store no-cache])

      max_age.to_i.seconds.clamp(MIN_FRESH, MAX_FRESH)
    end

    def ip_literal(host)
      IPAddr.new(host.to_s)
    rescue IPAddr::InvalidAddressError
      nil
    end

    def too_large(uri)
      "The client's description at #{uri.host} is larger than 64 KB, so Console does not read it."
    end

    def too_slow(uri)
      "#{uri.host} did not send the client's description within #{time_limit} seconds. Try again later."
    end

    def not_json(uri)
      "The client's description at #{uri.host} is not a JSON object."
    end
  end
end
