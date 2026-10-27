# frozen_string_literal: true

# What a view built on one EventStore answer shares: the answer, read once,
# nil when EventStore can't give it; event_store, whether it did and if not
# why; and a Project as the view's props name it.
module EventStoreView
  # Whether EventStore answered, and if not, why.
  def event_store
    answer
    @unavailable ? { available: false, error: @unavailable } : { available: true }
  end

  private

  # The answer the block reads from EventStore, read once, or nil when
  # EventStore can't answer, with @unavailable saying why it couldn't read
  # what, such as "Transactions".
  def event_store_answer(what)
    return @answer if defined?(@answer)

    @answer = yield
  rescue EventStore::Unavailable, EventStore::Rejected => e
    Rails.logger.error("#{what.upcase_first}: #{e.message}")
    @unavailable = "EventStore can't read the #{what}: #{e.message}"
    @answer = nil
  end

  def project_ref(project)
    { id: project.id, name: project.name, slug: project.slug }
  end
end
