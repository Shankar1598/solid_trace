module Api
  module V1
    class IssuesController < OrganizationCrudController

      def index
        issues = scoped_resources.order(created_at: :desc)

        if params[:level].present?
          issues = issues.where(level: params[:level])
        end

        if params[:status].present?
          issues = issues.where(status: params[:status])
        end

        if params[:query].present?
          issues = issues.where("title ILIKE ?", "%#{params[:query]}%")
        end

        render json: issues.map { |issue|
          {
            id: issue.id,
            title: issue.title,
            status: issue.status,
            level: issue.level,
            event_count: issue.events.count,
            created_at: issue.created_at,
            updated_at: issue.updated_at
          }
        }
      end

      def show
        issue = scoped_resources.find(params[:id])

        render json: {
          id: issue.id,
          title: issue.title,
          status: issue.status,
          level: issue.level,
          project_id: issue.project_id,
          created_at: issue.created_at,
          updated_at: issue.updated_at,
          events: issue.events.order(created_at: :desc).map { |event|
            {
              id: event.id,
              data: event.data,
              created_at: event.created_at
            }
          }
        }
      end

      def resolve
        issue = scoped_resources.find(params[:id])
        issue.update!(status: 1) # resolved

        render json: {
          id: issue.id,
          status: issue.status,
          message: "Issue resolved"
        }
      end

      def unresolve
        issue = scoped_resources.find(params[:id])
        issue.update!(status: 0) # unresolved

        render json: {
          id: issue.id,
          status: issue.status,
          message: "Issue unresolved"
        }
      end

      private

      def scoped_resources
        Issue.joins(project: :organization).where(organizations: { id: @current_org.id })
      end
    end
  end
end
