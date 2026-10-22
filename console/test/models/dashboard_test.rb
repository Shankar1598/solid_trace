# frozen_string_literal: true

require "test_helper"

class DashboardTest < ActiveSupport::TestCase
  setup do
    @dashboard = create(:dashboard)
  end

  test "a Dashboard opens with no filters" do
    assert_equal [], @dashboard.reload.filters
  end

  test "filters keep their order and known keys, with the name as an empty label" do
    @dashboard.update!(filters: [
      { "name" => "level", "label" => " Level ", "kind" => "select", "multiple" => "true", "color" => "red",
        "options" => { "from" => "list", "items" => [ { "label" => "Error", "value" => "error" }, { "label" => "", "value" => "warn" } ] } },
      { "name" => "q", "label" => "", "kind" => "search" }
    ])

    assert_equal [
      { "name" => "level", "label" => "Level", "kind" => "select", "multiple" => true,
        "options" => { "from" => "list", "items" => [ { "label" => "Error", "value" => "error" }, { "label" => "warn", "value" => "warn" } ] } },
      { "name" => "q", "label" => "q", "kind" => "search" }
    ], @dashboard.reload.filters
  end

  test "a filter's name is lowercase letters, digits and _, starting with a letter or _, at most 40 characters, and unique" do
    names = [ "service", "_env2", "Service", "2xx", "my-env", "a" * 41, "service", "" ]
    @dashboard.filters = names.map { |name| search(name) }

    assert_not @dashboard.valid?
    assert_equal %w[filters[2].name filters[3].name filters[4].name filters[5].name filters[6].name filters[7].name],
      @dashboard.errors.attribute_names.map(&:to_s)
    assert_includes @dashboard.errors[:"filters[6].name"], "is already a filter's name"
  end

  test "a filter's label is at most 100 characters and its kind select or search" do
    @dashboard.filters = [ search("a").merge("label" => "x" * 101), search("b").merge("kind" => "range") ]

    assert_not @dashboard.valid?
    assert_includes @dashboard.errors[:"filters[0].label"], "is too long (at most 100 characters)"
    assert_includes @dashboard.errors[:"filters[1].kind"], "must be select or search"
  end

  test "multiple and options are a select's only" do
    @dashboard.filters = [ search("q").merge("multiple" => true, "options" => list("a")) ]

    assert_not @dashboard.valid?
    assert @dashboard.errors[:"filters[0].multiple"].any?
    assert @dashboard.errors[:"filters[0].options"].any?
  end

  test "a select's fixed list has 1 to 200 options of strings at most 200 characters, with values unique and not empty" do
    @dashboard.filters = [
      select("none", { "from" => "list", "items" => [] }),
      select("many", list(*(1..201).map(&:to_s))),
      select("long", { "from" => "list", "items" => [ { "label" => "x" * 201, "value" => "y" * 201 } ] }),
      select("twice", list("error", "error")),
      select("blank", list(" ")),
      select("absent", nil)
    ]

    assert_not @dashboard.valid?
    errors = @dashboard.errors.to_hash.transform_keys(&:to_s)
    assert_equal %w[filters[0].options filters[1].options filters[2].options[0].label filters[2].options[0].value
      filters[3].options[1].value filters[4].options[0].value filters[5].options], errors.keys
    assert_includes errors["filters[3].options[1].value"], "is already an option's value"
  end

  test "a select's options can be a field of a Project of the Dashboard's Organization" do
    project = create(:project, organization: @dashboard.organization)
    @dashboard.update!(filters: [
      select("service", { "from" => "field", "project_id" => project.id.to_s, "field" => " service ", "items" => [] }, multiple: true)
    ])

    assert_equal [
      { "name" => "service", "label" => "service", "kind" => "select", "multiple" => true,
        "options" => { "from" => "field", "project_id" => project.id, "field" => "service" } }
    ], @dashboard.reload.filters
  end

  test "a field is at most 200 characters and not time, in a Project of the Dashboard's Organization" do
    project = create(:project, organization: @dashboard.organization)
    @dashboard.filters = [
      select("blank", field(project.id, " ")),
      select("long", field(project.id, "x" * 201)),
      select("time", field(project.id, "time")),
      select("foreign", field(create(:project).id, "service")),
      select("none", field(nil, "service")),
      select("odd", field("first", "service")),
      select("query", { "from" => "query" }),
      select("fine", field(project.id, "x" * 200))
    ]

    assert_not @dashboard.valid?
    errors = @dashboard.errors.to_hash.transform_keys(&:to_s)
    assert_equal %w[filters[0].options.field filters[1].options.field filters[2].options.field filters[3].options.project_id
      filters[4].options.project_id filters[5].options.project_id filters[6].options], errors.keys
    assert_includes errors["filters[2].options.field"], "can't be time"
    assert_includes errors["filters[3].options.project_id"], "must be a Project of the Dashboard's Organization"
    assert_includes errors["filters[6].options"], "must be a fixed list or a field"
  end

  test "a Dashboard has at most 20 filters, and each is an object" do
    @dashboard.filters = (1..21).map { |n| search("f#{n}") }
    assert_not @dashboard.valid?
    assert_includes @dashboard.errors[:filters], "are at most 20"

    @dashboard.filters = [ "level" ]
    assert_not @dashboard.valid?
    assert_includes @dashboard.errors[:"filters[0]"], "must be an object"

    @dashboard.filters = "level"
    assert_not @dashboard.valid?
    assert_includes @dashboard.errors[:filters], "must be a list"
  end

  private

  def search(name)
    { "name" => name, "label" => "", "kind" => "search" }
  end

  def select(name, options, multiple: false)
    { "name" => name, "label" => "", "kind" => "select", "multiple" => multiple, "options" => options }
  end

  def list(*values)
    { "from" => "list", "items" => values.map { |value| { "label" => value, "value" => value } } }
  end

  def field(project_id, name)
    { "from" => "field", "project_id" => project_id, "field" => name }
  end
end
