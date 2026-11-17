# frozen_string_literal: true

# The Sections of a Dashboard: add, rename, move and delete. Any member can do
# each, as with the Dashboard's charts. The page posts through Inertia and is
# redirected; JSON gets JSON back. A deleted Section's charts stay on the
# Dashboard with no Section.
class DashboardSectionsController < ApplicationController
  include DashboardView

  before_action :set_current_org
  before_action :set_dashboard
  before_action :set_section, only: %i[update destroy]

  # POST with section: {title, position}. With no position, it goes last.
  def create
    save(@dashboard.sections.new(title: section_params[:title]), notice: "Section added.", status: :created)
  end

  # PATCH with section: {title, position}, each only to change it. A position
  # moves the Section there, from 0, and moves the others to make room.
  def update
    @section.title = section_params[:title] if section_params.key?(:title)
    save(@section, notice: "Section saved.")
  end

  def destroy
    @section.destroy
    respond_to do |format|
      format.html { redirect_to dashboard_page(@dashboard), notice: "Section deleted. Its charts are now in no Section." }
      format.json { head :no_content }
    end
  end

  private

  def set_dashboard
    @dashboard = @current_org.dashboards.find(params[:dashboard_id])
  end

  def set_section
    @section = @dashboard.sections.find(params[:id])
  end

  def section_params
    @section_params ||= params.require(:section).permit(:title, :position)
  end

  def save(section, notice:, status: :ok)
    saved = begin
      section.save_at!(section_params[:position].presence)
    rescue ActiveRecord::RecordInvalid
      false
    end
    respond_to do |format|
      if saved
        format.html { redirect_to dashboard_page(@dashboard), notice: notice }
        format.json { render json: DashboardSectionSerializer.new(section).as_json, status: status }
      else
        format.html { redirect_to dashboard_page(@dashboard), inertia: { errors: section.errors.to_hash } }
        format.json { render json: { errors: section.errors.to_hash }, status: :unprocessable_content }
      end
    end
  end
end
