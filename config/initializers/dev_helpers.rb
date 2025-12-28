# frozen_string_literal: true

if Rails.env.development?
  def byebug
    debugger
  end

  def disable_pager!
    IRB.conf[:USE_PAGER] = false
  end

  def trigger_test_events!
    begin
      begin
        1 / 0
      rescue ZeroDivisionError => exception
        # Sentry.capture_exception(exception)
        raise "Error during division"
      end
    rescue => e
      Sentry.capture_exception(e)
    end

    Sentry.capture_message("test message")
  end
end

if Rails.env.production?
  def debugger
    Rails.logger.warn("Debugger called in production")
    Sentry.capture_message("Debugger called in production")
    nil
  end
end

Object.class_eval do
  def ps
    puts self
  end

  def dj
    to_json.ps
  end

  def djp
    to_json(pretty: true).ps
  end
end
