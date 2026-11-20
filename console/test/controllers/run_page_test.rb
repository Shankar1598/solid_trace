# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Run page: one Run from its run events, as EventStore's GET
# /runs/:run_id answers them. It shows the state with its reason, the
# trigger and actor, the Project, repository, folder and base branch, the
# PR, the Executions, the events and interventions, the last report with
# its checks and Evidence files, the Agent sessions and the CLI commands.
# The Stop button calls the Dispatcher with Console's factory token for the
# signed-in user. Someone outside the Organization gets 404.
class RunPageTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  DISPATCHER = "http://10.100.0.3:8080"
  SESSION = "7d1e5a90-2c4b-4f7a-8e61-5b3c9d0a2e17"

  setup do
    @organization = create(:organization, slug: "acme")
    @admin = create(:user, name: "Ada")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user, name: "Bob")
    create(:organization_user, organization: @organization, user: @member, role: :member)
    create(:project, organization: @organization, name: "Shop", slug: "shop")
    @project = AgentSessionsSettings.turn_on(organization: @organization, user: @admin,
      entries: [ "github.com/acme/shop" ], confirmed: "1").project
    travel_to Time.utc(2026, 10, 3, 12, 0, 0)
  end

  def snapshot(state, reason: "", pr: { "url" => "", "outcome" => "" }, executions: 1)
    {
      "run.id" => "run_1", "run.state" => state, "run.reason" => reason, "run.trigger" => "cli",
      "run.title" => "Fix the cart total", "run.executions" => executions, "run.base_branch" => "develop",
      "run.pr.url" => pr["url"], "run.pr.outcome" => pr["outcome"], "factory.project" => "shop",
      "repository" => "bitbucket.org/acme/shop", "folder" => "web",
    }
  end

  def event(seq, time, kind, state, actor: { kind: "person", id: "7", name: "Ana" }, **details)
    attributes = snapshot(state).merge("run.seq" => seq, "event.kind" => kind, "event.id" => "run_1-#{seq}",
      "actor.kind" => actor[:kind], "actor.id" => actor[:id], "actor.name" => actor[:name])
    { id: "log-#{seq}", time: time, level: "info", message: "Run run_1: #{kind}", service: "bitbucket.org/acme/shop",
      attributes: attributes.merge(details.transform_keys(&:to_s)) }
  end

  def checks(lint_status: 1)
    [
      { name: "unit", command: "bun test", exit_status: 0, time: "2026-10-03T10:20:00Z", output_tail: "12 pass" },
      { name: "lint", command: "bun lint", exit_status: lint_status, time: "2026-10-03T10:21:00Z", output_tail: "1 error" }
    ]
  end

  def events(report_checks: checks, last_state: "done")
    started = { "execution.id" => "x1", "execution.account" => "team-2", "execution.vm" => "agent-vm-7", "agent.session_id" => SESSION }
    [
      event(1, "2026-10-03T10:00:00Z", "created", "queued"),
      event(2, "2026-10-03T10:00:01Z", "queued", "queued"),
      event(3, "2026-10-03T10:01:00Z", "execution_started", "running", **started),
      event(4, "2026-10-03T10:05:00Z", "message_accepted", "running", actor: { kind: "person", id: "9", name: "Cy" },
        "message.text" => "Also fix the tax line"),
      event(5, "2026-10-03T10:30:00Z", "report", last_state, "agent.session_id" => SESSION, "report.outcome" => "done",
        "report.summary" => "Fixed the total.", "report.checks" => report_checks,
        "report.evidence" => [ "evidence/cart.png", "evidence/report.md" ]),
      event(6, "2026-10-03T10:31:30Z", "execution_ended", last_state, **started, "execution.reason" => "report",
        "execution.exit_code" => 0)
    ]
  end

  def stub_run(list = events, pr_url: "https://bitbucket.org/acme/shop/pull-requests/12", pr_outcome: "", closed: false)
    last = list.last[:attributes]
    run = { id: "run_1", seq: last["run.seq"], time: list.last[:time], kind: last["event.kind"], state: last["run.state"],
            reason: last["run.reason"], project: "shop", repository: "bitbucket.org/acme/shop", folder: "web",
            base_branch: "develop", trigger: "cli", title: "Fix the cart total", actor: { kind: "person", id: "7", name: "Ana" },
            pr: { url: pr_url, outcome: pr_outcome }, executions: 1,
            active: !closed, closed: closed }
    stub_request(:get, "#{EventStore.base_url}/api/#{@project.id}/runs/run_1")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { run: run, events: list }.to_json)
  end

  def stub_evidence(path, body, type)
    stub_request(:get, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{SESSION}/evidence")
      .with(query: { path: path })
      .to_return(status: 200, headers: { "Content-Type" => type }, body: body)
  end

  def page
    get run_path(org_slug: "acme", id: "run_1"), headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "Runs/Show", response.parsed_body["component"]
    response.parsed_body.dig("props", "run")
  end

  test "a member sees each field of the Run from its events" do
    stub_run
    report = stub_evidence("evidence/report.md", "# Report\n\nFixed the total.\n\nThe whole text.\n", "text/markdown; charset=utf-8")
    sign_in_as(@member)

    view = page

    run = view["run"]
    assert_equal [ "run_1", "done", "cli", "Fix the cart total", "shop", "bitbucket.org/acme/shop", "web", "develop" ],
      run.values_at("id", "state", "trigger", "title", "project", "repository", "folder", "base_branch")
    assert_equal "Ana", run.dig("actor", "name")
    assert_equal "https://bitbucket.org/acme/shop/pull-requests/12", run.dig("pr", "url")
    assert_equal({ "name" => "Shop", "slug" => "shop" }, view["project"])
    assert_equal [ 1, 2, 3, 4, 5, 6 ], view["events"].map { |e| e["seq"] }
    message = view["events"][3]
    assert_equal [ "message_accepted", "Also fix the tax line", true, "Cy" ],
      [ message["kind"], message["detail"], message["intervention"], message.dig("actor", "name") ]
    assert_equal false, view["events"][0]["intervention"]
    assert_equal "2026-10-03T10:00:00Z", view["events"][0]["time"]
    assert_equal [ SESSION ], view["agent_sessions"].map { |s| s["id"] }
    assert_equal agent_session_path(org_slug: "acme", id: SESSION), view["agent_sessions"].first["path"]
    assert_equal({ "message" => "solidtrace message run_1 \"<your message>\"", "take_over" => "solidtrace take-over run_1",
                   "stop" => "solidtrace stop run_1" }, view["commands"])
    assert_requested report
  end

  test "each Execution shows its start, end, reason, account, VM and duration" do
    stub_run
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    sign_in_as(@member)

    execution = page["executions"].sole

    assert_equal [ "x1", "2026-10-03T10:01:00Z", "2026-10-03T10:31:30Z", "report", "team-2", "agent-vm-7", 1830, 0 ],
      execution.values_at("id", "start", "end", "reason", "account", "vm", "duration_seconds", "exit_code")
  end

  test "a taken-over Run shows its VM and the attach command, and the take-over's Execution is interactive" do
    attach = "docker exec -i -u engineer factory-local-agent-machine-1 sudo -u agent hc connect"
    place = { "execution.vm" => "agent-machine-1", "execution.attach" => attach }
    list = events(last_state: "running").first(4) + [
      event(5, "2026-10-03T11:00:00Z", "execution_ended", "running", "execution.id" => "x1", "execution.vm" => "agent-machine-1",
        "execution.reason" => "taken over", "execution.exit_code" => 130),
      event(6, "2026-10-03T11:00:01Z", "take_over", "taken_over", actor: { kind: "person", id: "12", name: "Dee" },
        "execution.id" => "x2", "take_over.from_execution" => "x1", **place),
      event(7, "2026-10-03T11:00:02Z", "execution_started", "taken_over", actor: { kind: "person", id: "12", name: "Dee" },
        "execution.id" => "x2", "execution.form" => "interactive", "execution.account" => "team-2", **place)
    ]
    stub_run(list)
    sign_in_as(@member)

    view = page

    assert_equal({ "vm" => "agent-machine-1", "attach" => attach, "by" => { "kind" => "person", "id" => "12", "name" => "Dee" } },
      view["take_over"])
    assert_equal [ [ "x1", "headless", nil ], [ "x2", "interactive", attach ] ],
      view["executions"].map { |e| e.values_at("id", "form", "attach") }
  end

  test "a Run that is not taken over has no take-over" do
    stub_run
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    sign_in_as(@member)

    view = page

    assert_nil view["take_over"]
  end

  test "an Execution that has not ended has no end and no duration" do
    stub_run(events.first(3))
    sign_in_as(@member)

    execution = page["executions"].sole

    assert_equal [ "x1", "2026-10-03T10:01:00Z", nil, nil ], execution.values_at("id", "start", "end", "duration_seconds")
  end

  test "each intervention shows with its actor" do
    list = events + [ event(7, "2026-10-03T11:00:00Z", "stop", "stopped", actor: { kind: "person", id: "12", name: "Dee" }),
                      event(8, "2026-10-03T11:01:00Z", "take_over", "taken_over", actor: { kind: "person", id: "7", name: "Ana" }) ]
    stub_run(list)
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    sign_in_as(@member)

    interventions = page["interventions"]

    assert_equal [ [ "message_accepted", "Cy" ], [ "stop", "Dee" ], [ "take_over", "Ana" ] ],
      interventions.map { |e| [ e["kind"], e.dig("actor", "name") ] }
  end

  test "the last report shows its checks with pass or fail and the output tail, its Evidence files, and evidence/report.md in full" do
    stub_run
    text = "# Report\n\n#{"A long line of the summary. " * 400}\n\n## Cause report\n\nThe cart rounded each line.\n"
    stub_evidence("evidence/report.md", text, "text/markdown; charset=utf-8")
    sign_in_as(@member)

    report = page["report"]

    assert_equal [ [ "unit", true, "12 pass" ], [ "lint", false, "1 error" ] ],
      report["checks"].map { |c| c.values_at("name", "passed", "output_tail") }
    assert_equal [ "bun lint", 1 ], report["checks"].last.values_at("command", "exit_status")
    assert_equal "Fixed the total.", report["summary"]
    image, markdown = report["evidence"]
    assert_equal [ "evidence/cart.png", true ], image.values_at("path", "image")
    assert_equal run_evidence_path(org_slug: "acme", id: "run_1", path: "evidence/cart.png"), image["url"]
    assert_equal [ "evidence/report.md", false ], markdown.values_at("path", "image")
    assert_equal text, report["report_md"]
    assert_equal false, report["no_evidence"]
  end

  test "a done report with no check that passed shows 'no Evidence'" do
    failed = checks.map { |c| c.merge(exit_status: 2) }
    stub_run(events(report_checks: failed))
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    sign_in_as(@member)

    assert_equal true, page.dig("report", "no_evidence")
  end

  test "a check with no exit status has not passed, so a done report with only that check shows 'no Evidence'" do
    stub_run(events(report_checks: [ { name: "unit", command: "bun test", time: "2026-10-03T10:20:00Z", output_tail: "" } ]))
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    sign_in_as(@member)

    report = page["report"]

    assert_equal [ nil, false ], report["checks"].sole.values_at("exit_status", "passed")
    assert_equal true, report["no_evidence"]
  end

  test "the Run's Project is the Project of its own Organization with that slug, never another Organization's" do
    stub_run
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    @organization.projects.find_by!(slug: "shop").update!(slug: "store")
    create(:project, organization: create(:organization, slug: "other"), name: "Other Shop", slug: "shop")
    sign_in_as(@member)

    assert_nil page["project"]
  end

  test "a report whose evidence/report.md is not uploaded yet says so, and the rest of the page shows" do
    stub_run
    stub_request(:get, "#{EventStore.base_url}/api/#{@project.id}/agent-sessions/#{SESSION}/evidence")
      .with(query: { path: "evidence/report.md" }).to_return(status: 404, body: { error: "no such file" }.to_json)
    sign_in_as(@member)

    report = page["report"]

    assert_nil report["report_md"]
    assert_match "not uploaded", report["report_md_error"]
  end

  test "when EventStore cannot read the Run, the page shows the EventStore error and an empty Run row" do
    stub_request(:get, "#{EventStore.base_url}/api/#{@project.id}/runs/run_1").to_return(status: 503, body: "down")
    sign_in_as(@member)

    view = page

    assert_equal false, view.dig("event_store", "available")
    assert view.dig("event_store", "error").present?
    run = view["run"]
    assert_equal "run_1", run["id"]
    assert_equal({ "kind" => "", "id" => "", "name" => "" }, run["actor"])
    assert_equal({ "url" => "", "outcome" => "" }, run["pr"])
    assert_equal false, view.dig("stop", "can")
  end

  test "an Issue Run's report with a cause report and no PR shows the cause report, and is not 'no Evidence'" do
    list = events(report_checks: [])
    list[4][:attributes]["report.cause_report"] = "The cart rounded each line."
    stub_run(list, pr_url: "")
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    sign_in_as(@member)

    report = page["report"]

    assert_equal "The cart rounded each line.", report["cause_report"]
    assert_equal false, report["no_evidence"]
  end

  test "an Issue Run's report with a cause report, a PR and no check that passed is 'no Evidence'" do
    list = events(report_checks: checks(lint_status: 1).map { |c| c.merge(exit_status: 1) })
    list[4][:attributes]["report.cause_report"] = "The cart rounded each line."
    stub_run(list)
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    sign_in_as(@member)

    report = page["report"]

    assert_equal "The cart rounded each line.", report["cause_report"]
    assert_equal true, report["no_evidence"]
  end

  test "an Evidence file of the report shows: an image inline with its type, another file as a download" do
    stub_run
    stub_evidence("evidence/cart.png", "\x89PNG the image".b, "image/png")
    stub_evidence("evidence/report.md", "# Report", "text/markdown; charset=utf-8")
    sign_in_as(@member)

    get run_evidence_path(org_slug: "acme", id: "run_1", path: "evidence/cart.png")

    assert_response :success
    assert_equal "image/png", response.media_type
    assert_equal "\x89PNG the image".b, response.body.b
    assert_match "inline", response.headers["Content-Disposition"]
    assert_equal "nosniff", response.headers["X-Content-Type-Options"]

    get run_evidence_path(org_slug: "acme", id: "run_1", path: "evidence/report.md")

    assert_response :success
    assert_match "attachment", response.headers["Content-Disposition"]
  end

  test "a file that no report of the Run names is 404, and EventStore is not asked" do
    stub_run
    sign_in_as(@member)

    get run_evidence_path(org_slug: "acme", id: "run_1", path: "evidence/other.png")

    assert_response :not_found
    assert_not_requested :get, %r{/evidence}
  end

  test "a Run that EventStore does not have is 404" do
    stub_request(:get, "#{EventStore.base_url}/api/#{@project.id}/runs/run_2").to_return(status: 404, body: { error: "No such Run" }.to_json)
    sign_in_as(@member)

    get run_path(org_slug: "acme", id: "run_2")

    assert_response :not_found
  end

  test "the Stop button shows only with a Dispatcher URL, on a Run that can stop" do
    stub_run(events.first(4))
    sign_in_as(@member)

    assert_equal false, page.dig("stop", "can")

    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)

    assert_equal true, page.dig("stop", "can")
    assert_equal stop_run_path(org_slug: "acme", id: "run_1"), page.dig("stop", "path")
  end

  test "a Run that is done cannot stop" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    stub_run
    stub_evidence("evidence/report.md", "Fixed.", "text/markdown")
    sign_in_as(@member)

    assert_equal false, page.dig("stop", "can")
  end

  test "a closed Run cannot stop, also when its last state is running" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    stub_run(events.first(4), pr_outcome: "declined", closed: true)
    sign_in_as(@member)

    assert_equal "running", page.dig("run", "state")
    assert_equal false, page.dig("stop", "can")
  end

  test "the Stop button sends a stop with a factory token for the signed-in user, and the page shows the stopped state" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    stub_run(events.first(4))
    stop = stub_request(:post, "#{DISPATCHER}/api/v1/runs/run_1/stop")
      .to_return(status: 200, headers: { "Content-Type" => "application/json" }, body: { id: "run_1", state: "stopped" }.to_json)
    sign_in_as(@member)

    post stop_run_path(org_slug: "acme", id: "run_1")

    assert_redirected_to run_path(org_slug: "acme", id: "run_1")
    assert_requested(:post, "#{DISPATCHER}/api/v1/runs/run_1/stop") do |request|
      token = Oauth::ConsoleToken.find_live(request.headers["Authorization"].to_s.delete_prefix("Bearer "))
      token && token.user == @member && token.organization == @organization
    end
    assert_requested stop
    assert_equal "stopped", page.dig("stop", "sent_state")
  end

  test "a stop that the Dispatcher refuses shows its error" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    stub_request(:post, "#{DISPATCHER}/api/v1/runs/run_1/stop")
      .to_return(status: 409, headers: { "Content-Type" => "application/json" }, body: { error: "the Run is closed" }.to_json)
    sign_in_as(@member)

    post stop_run_path(org_slug: "acme", id: "run_1")

    assert_redirected_to run_path(org_slug: "acme", id: "run_1")
    assert_equal "The Dispatcher did not stop the Run: the Run is closed", flash[:alert]
  end

  test "a stop with no Dispatcher URL is 404 and calls nothing" do
    sign_in_as(@member)

    post stop_run_path(org_slug: "acme", id: "run_1")

    assert_response :not_found
  end

  test "a member who is not in the Organization gets 404 for the page, the Evidence and the stop" do
    InstanceSetting.current.update!(dispatcher_url: DISPATCHER)
    sign_in_as(create(:user))

    get run_path(org_slug: "acme", id: "run_1")
    assert_response :not_found
    get run_evidence_path(org_slug: "acme", id: "run_1", path: "evidence/cart.png")
    assert_response :not_found
    post stop_run_path(org_slug: "acme", id: "run_1")
    assert_response :not_found

    assert_not_requested :any, %r{#{DISPATCHER}}
    assert_not_requested :any, %r{/runs/run_1}
  end
end
