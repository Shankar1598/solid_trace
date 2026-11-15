# frozen_string_literal: true

# The Console side of the factory demo (bin/factory-demo, factory/internal/flowstack): the settings
# that the demo's Dispatcher needs, and the steps that put them back. Each task prints one JSON line
# last. They run only in development and test, on the dev stack's database.
namespace :factory_demo do
  demo = lambda do
    abort "factory_demo runs only in development and test" unless Rails.env.local?
  end

  value = ->(text) { text.to_s.presence }

  desc "Set up the demo: Agent sessions on, the Dispatcher's token and an Admin's token. " \
       "DEMO_ORG, DEMO_PROJECT, DEMO_ADMIN (an Admin's email) and DEMO_CONSOLE_URL"
  task up: :environment do
    demo.call
    org = Organization.find_by(slug: ENV.fetch("DEMO_ORG")) or abort "no Organization #{ENV["DEMO_ORG"].inspect}"
    project = org.projects.find_by(slug: ENV.fetch("DEMO_PROJECT")) or abort "no Project #{ENV["DEMO_PROJECT"].inspect} in #{org.slug}"
    admin = User.find_by(email: ENV.fetch("DEMO_ADMIN")) or abort "no user #{ENV["DEMO_ADMIN"].inspect}"
    membership = org.organization_users.find_by(user: admin)
    abort "#{admin.email} is not an Admin of #{org.slug}" unless membership&.admin?

    settings = InstanceSetting.current
    old = { console_url: value.(settings.console_url), dispatcher_url: value.(settings.dispatcher_url) }
    console_url = ENV.fetch("DEMO_CONSOLE_URL")

    # Agent sessions stay on after the demo: the Runs and their Agent sessions are there to look at.
    turned_on = org.agent_project.nil?
    if turned_on
      result = AgentSessionsSettings.turn_on(organization: org, user: admin, entries: [ "factory-demo.example/demo/*" ], confirmed: true)
      abort "Agent sessions: #{result.errors.inspect}" unless result.done?
    end
    dispatcher, dispatcher_secret = ApiToken.issue!(user: admin, name: "Factory demo: Dispatcher", scopes: Oauth::Scopes::DISPATCHER,
                                                    organization_id: org.id)
    person, person_secret = ApiToken.issue!(user: admin, name: "Factory demo: the person who starts the Runs", scopes: [ "factory" ],
                                            organization_id: org.id)
    # The last change, so that a failure before it changes no setting.
    settings.update!(console_url: console_url)
    print({ org: org.slug, project: project.slug, admin: admin.email, console_url: console_url, old: old, turned_on: turned_on,
            dispatcher_token: { id: dispatcher.id, secret: dispatcher_secret },
            person_token: { id: person.id, secret: person_secret } }.to_json)
  end

  desc "Set the Dispatcher URL of the running demo: DEMO_DISPATCHER_URL"
  task dispatcher_url: :environment do
    demo.call
    InstanceSetting.current.update!(dispatcher_url: ENV.fetch("DEMO_DISPATCHER_URL"))
  end

  # DEMO_STATE is the JSON that the demo kept: what up printed, with dispatcher_url (the one that the demo
  # set). A setting that someone else changed since stays as it is. Safe to run more than one time.
  desc "Put back what the demo changed: DEMO_STATE"
  task down: :environment do
    demo.call
    state = JSON.parse(ENV.fetch("DEMO_STATE"))
    settings = InstanceSetting.current
    changes = {}
    changes[:console_url] = state.dig("old", "console_url") if value.(settings.console_url) == state["console_url"]
    changes[:dispatcher_url] = state.dig("old", "dispatcher_url") if state["dispatcher_url"] && value.(settings.dispatcher_url) == state["dispatcher_url"]
    settings.update!(changes) if changes.any?
    ApiToken.where(id: [ state.dig("dispatcher_token", "id"), state.dig("person_token", "id") ].compact).destroy_all
  end
end
