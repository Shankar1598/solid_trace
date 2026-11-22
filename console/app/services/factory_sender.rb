# frozen_string_literal: true

# The Webhook sender of a 'Factory' Integration, which Console makes and
# deletes at the Dispatcher's API as the signed-in Admin (W6). The sender
# has no list of Projects, so it starts Runs in each Project of the
# Organization of the Admin's token, and in no other Organization. It is
# made as the Factory Integration's sender (factory: true): only its
# deliveries can have the solidtrace.* attributes that start an Issue or a
# Monitor Run.
#
#   id, secret = FactorySender.new(user: current_user, organization: @current_org).create("Factory")
#
# Each call raises Dispatcher::Refused or Dispatcher::Unavailable, whose
# message says what went wrong.
class FactorySender
  PATH = "/api/v1/senders"

  def initialize(user:, organization:, settings: InstanceSetting.current)
    @dispatcher = Dispatcher.new(user: user, organization: organization, settings: settings)
  end

  # Makes a sender named name. Returns its id and its secret, which the
  # Dispatcher gives in this answer only.
  def create(name)
    answer = @dispatcher.request(:post, PATH, body: { name: name, factory: true })
    id = answer["id"] if answer.is_a?(Hash)
    secret = answer["secret"] if answer.is_a?(Hash)
    unless id.is_a?(String) && id.present? && secret.is_a?(String) && secret.present?
      raise Dispatcher::Unavailable, "The Dispatcher answered with no sender id or no secret."
    end

    [ id, secret ]
  end

  # Deletes the sender id. A sender that the Dispatcher no longer has is
  # already deleted.
  def delete(id)
    @dispatcher.request(:delete, "#{PATH}/#{ERB::Util.url_encode(id)}")
  rescue Dispatcher::Refused => e
    raise unless e.status == 404
  end
end
