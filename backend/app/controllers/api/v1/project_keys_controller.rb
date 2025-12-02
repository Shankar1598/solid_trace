module Api
  module V1
    class ProjectKeysController < OrganizationCrudController
      def index
        keys = scoped_resources

        render json: keys.map { |key|
          {
            id: key.id,
            public_key: key.public_key,
            secret_key: key.secret_key,
            dsn: "http://#{key.public_key}@localhost:3000/#{key.project_id}"
          }
        }
      end

      def create
        key = scoped_resources.create!(
          public_key: SecureRandom.hex(16),
          secret_key: SecureRandom.hex(16)
        )

        render json: {
          id: key.id,
          public_key: key.public_key,
          secret_key: key.secret_key,
          dsn: "http://#{key.public_key}@localhost:3000/#{key.project_id}"
        }, status: :created
      end

      def update
        key = scoped_resources.find(params[:id])
        # For now, update just rotates the keys
        key.update!(
          public_key: SecureRandom.hex(16),
          secret_key: SecureRandom.hex(16)
        )

        render json: {
          id: key.id,
          public_key: key.public_key,
          secret_key: key.secret_key,
          dsn: "http://#{key.public_key}@localhost:3000/#{key.project_id}"
        }
      end

      def destroy
        key = scoped_resources.find(params[:id])
        key.destroy
        head :no_content
      end

      private

      def scoped_resources
        @current_org.projects.find_by(slug: params[:project_slug]).project_keys
      end
    end
  end
end
