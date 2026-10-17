# frozen_string_literal: true

module McpTools
  # Invites someone to an Organization by email, as its Members page does
  # (MemberInvite). For the Organization's admins.
  class InviteMember < Base
    description "Invites someone to an Organization as a member, by email. Someone new to SolidTrace gets a user " \
                "and an email with their password; an archived member is brought back. Only an Organization admin can."
    writes!(open_world: true)
    takes_organization
    argument :email, { type: "string", description: "The email to invite." }, required: true
    argument :name, { type: "string", description: "Their name, for someone new to SolidTrace. The email's name when left out." }

    def run
      invite = MemberInvite.call(organization: organization, inviter: user, email: arguments[:email], name: arguments[:name])
      raise ToolError, "Not invited: #{invite.error}." unless invite.done?

      member = invite.membership
      json({
        organization: organization.slug,
        member: { name: member.user.name, email: member.user.email, role: member.role },
        outcome: invite.outcome.to_s,
        emailed: invite.emailed,
      }, link: links.members(organization))
    rescue Pundit::NotAuthorizedError
      raise ToolError, "Only an Organization admin can invite members."
    end
  end
end
