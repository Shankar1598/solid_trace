# frozen_string_literal: true

# An admin's save of a Project's Log transform, from its Log transform page
# or the MCP server's update_log_transform. EventStore's check compiles the
# program first, and a program with errors saves nothing. A program that
# compiles becomes the Project's newest LogTransformVersion, which Log
# processing runs from its next pass; Logs already stored don't change.
# Restoring an old version is saving its program again. A blank program
# turns the Log transform off, so it saves without a check.
#
# Only the Organization's admins save (ProjectPolicy#update?): anyone else's
# save raises Pundit::NotAuthorizedError before it checks or changes
# anything.
class LogTransformSave
  # What the save did: :saved, with the new version; :unchanged, with the
  # current version (nil when there's none), when the program is the current
  # one; or nil, with the check's errors, each {"message", "code", "start",
  # "end", "text"}.
  Result = Data.define(:outcome, :version, :errors) do
    def done?
      errors.empty?
    end
  end

  def self.call(project:, user:, program:)
    new(project: project, user: user, program: program.to_s).call
  end

  def initialize(project:, user:, program:)
    @project = project
    @user = user
    @program = program
  end

  def call
    Pundit.authorize(@user, @project, :update?)

    current = @project.log_transform
    return Result.new(outcome: :unchanged, version: current, errors: []) if current&.program.to_s == @program

    errors = check_errors
    return Result.new(outcome: nil, version: nil, errors: errors) if errors.any?

    version = @project.log_transform_versions.create!(program: @program, user: @user)
    Result.new(outcome: :saved, version: version, errors: [])
  end

  private

  def check_errors
    return [] if @program.blank?

    Array(EventStore.check_log_transform(project_id: @project.id, program: @program)["errors"])
  end
end
