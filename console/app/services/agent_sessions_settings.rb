# frozen_string_literal: true

# An Admin's changes on Organization settings → Agent sessions: the turn-on,
# which makes the Organization's Agent Project, the saves of its captured
# repositories, and the saves of its Evidence Retention. The turn-on needs at least one captured repository. The
# turn-on, and each save that adds entries, needs the Admin's confirm of
# NOTICE, which is recorded as a CaptureConfirmation with who, when and the
# entries it was for. A save that only removes entries needs none. The
# caller checks that the user is an Admin.
class AgentSessionsSettings
  NOTICE = "I told the engineers whose Agent sessions in these repositories upload, " \
           "and that each member of the Organization can read them"

  # The Agent Project's Retention when it is made: 6 months (story 38).
  RETENTION_DAYS = 180

  NAME = "Agent sessions"
  DESCRIPTION = "The Agent sessions of the Organization's Runs, and of its engineers in its captured repositories: " \
                "one Log for each transcript line."

  # project is the Agent Project, also when nothing was saved; errors is a
  # hash of messages by field, empty when the change was saved.
  Result = Data.define(:project, :errors) do
    def done?
      errors.empty?
    end
  end

  def self.turn_on(organization:, user:, entries:, confirmed:)
    new(user: user, confirmed: confirmed).turn_on(organization, entries)
  end

  def self.save(project:, user:, entries:, confirmed:)
    new(user: user, confirmed: confirmed).save(project, entries)
  end

  # A refused save keeps the project's saved Evidence Retention, so the page
  # shows it with the errors.
  def self.save_evidence_retention(project:, days:)
    return Result.new(project: project, errors: {}) if project.update(evidence_retention_days: days)

    errors = project.errors.to_hash
    project.restore_attributes
    Result.new(project: project, errors: errors)
  end

  def initialize(user:, confirmed:)
    @user = user
    @confirmed = confirmed
  end

  def turn_on(organization, entries)
    project = organization.agent_project
    return Result.new(project: project, errors: { base: [ "Agent sessions are already on." ] }) if project

    project = organization.projects.build(
      agent: true, name: free_name(organization), description: DESCRIPTION,
      log_retention_days: RETENTION_DAYS, captured_repositories: entries,
    )
    valid = project.valid?
    errors = {}
    errors[:captured_repositories] = [ "need at least one repository to turn on Agent sessions" ] if project.captured_repositories.empty?
    check_confirm(errors)
    return refused(project, errors) unless valid && errors.empty?

    Project.transaction do
      project.save!
      project.capture_confirmations.create!(user: @user, entries: project.captured_repositories)
    end
    Result.new(project: project, errors: {})
  rescue ActiveRecord::RecordNotUnique
    Result.new(project: organization.reload.agent_project, errors: { base: [ "Agent sessions are already on." ] })
  end

  def save(project, entries)
    before = project.captured_repositories
    project.captured_repositories = entries
    added = project.captured_repositories - before
    valid = project.valid?
    errors = {}
    check_confirm(errors) if added.any?
    return refused(project, errors) unless valid && errors.empty?

    Project.transaction do
      project.save!
      project.capture_confirmations.create!(user: @user, entries: added) if added.any?
    end
    Result.new(project: project, errors: {})
  end

  private

  # Adds the error for a missing confirm to errors.
  def check_confirm(errors)
    return if ActiveModel::Type::Boolean.new.cast(@confirmed)

    errors[:confirmed] = [ "is needed: confirm that you told the engineers" ]
  end

  def refused(project, errors)
    Result.new(project: project, errors: project.errors.to_hash.merge(errors))
  end

  # The Agent Project's name, with a number when another Project's slug is
  # agent-sessions already.
  def free_name(organization)
    taken = organization.projects.pluck(:slug)
    name = NAME
    number = 1
    name = "#{NAME} #{number += 1}" while taken.include?(name.parameterize)
    name
  end
end
