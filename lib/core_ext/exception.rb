# frozen_string_literal: true

class Exception
  def log_data
    data = {
      class: self.class,
      message: message,
      backtrace: backtrace,
    }
    if cause
      data[:cause] = cause.log_data
    end
    data
  end
end
