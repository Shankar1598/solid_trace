# frozen_string_literal: true

# Takes the Sentry envelopes of a signed-in user's browser, the Console
# frontend's own errors, and sends them to Console's own Sentry, the Meta
# Project on an install, as Sentry's tunnel option expects (docs/traces.md),
# with their frames un-minified (BrowserSourceMaps). So the browser never
# reaches EventStore and never holds the Meta Project's key. Its answer is 202
# when it sent the envelope, and 204 when Console sends no Issues.
class BrowserErrorsController < ApplicationController
  # Larger than any one error's envelope; a body past it is refused unread.
  MAX_BYTES = 1.megabyte

  def create
    return head :content_too_large if request.content_length.to_i > MAX_BYTES

    body = request.raw_post
    return head :content_too_large if body.bytesize > MAX_BYTES

    head(ConsoleSentry.send_browser_envelope(body) ? :accepted : :no_content)
  rescue Sentry::ExternalError => e
    Rails.logger.warn("BrowserErrorsController couldn't send a browser's envelope: #{e.message}")
    head :bad_gateway
  end
end
