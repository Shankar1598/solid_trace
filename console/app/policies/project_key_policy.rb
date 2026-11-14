# frozen_string_literal: true

# A Project's keys send Logs and Events to it, so only the Organization's
# admins make, rotate and delete them, in the UI and the MCP server alike.
# The Agent Project gets no key: Agent sessions and run events come in with
# API tokens, and a key would let any sender write Logs into it.
class ProjectKeyPolicy < ApplicationPolicy
  AGENT_PROJECT = "The Agent Project has no keys: Agent sessions and run events come in with API tokens."

  def create?
    admin? && !record.project.agent?
  end

  def rotate?
    admin?
  end

  alias_method :destroy?, :rotate?

  private

  def admin?
    record.project.organization.organization_users.find_by(user: user)&.admin? || false
  end
end
