# Multi-stage build example for webapp.build
FROM docker.io/library/nginx:alpine

# Build arguments from webapp.build
ARG VERSION
ARG ENV

# Label the image with build arguments
LABEL version=$VERSION \
      environment=$ENV \
      description="Example webapp built with Quadlet"

# Copy content (if any exists in build context)
# COPY ./html /usr/share/nginx/html

# Expose HTTP port
EXPOSE 80

# Default nginx command
CMD ["nginx", "-g", "daemon off;"]
