# frozen_string_literal: true

require "test_helper"

# The invite email, with its Invite link to Console's URL as it is when sent.
class OrganizationMailerTest < ActionMailer::TestCase
  include ActiveJob::TestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user, invited: true)
    @inviter = create(:user)
  end

  test "the invite link is to the address the admin invited from when no Console URL is saved or set" do
    perform_enqueued_jobs do
      OrganizationMailer.invite_link_email(user: @user, organization: @organization, invited_by: @inviter,
        request_url: "http://console.test").deliver_later
    end

    assert_match %r{http://console\.test/invites/\S+\?org=#{@organization.slug}}, ActionMailer::Base.deliveries.last.text_part.body.to_s
  end

  test "an email queued with the old console_url: keyword still sends, with the right link" do
    perform_enqueued_jobs do
      OrganizationMailer.invite_link_email(user: @user, organization: @organization, invited_by: @inviter,
        console_url: "http://console.test").deliver_later
    end

    assert_match %r{http://console\.test/invites/\S+\?org=#{@organization.slug}}, ActionMailer::Base.deliveries.last.text_part.body.to_s
  end
end
