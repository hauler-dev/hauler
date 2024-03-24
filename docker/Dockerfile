# Use Alpine as base image
FROM alpine:3.19

# Set working directory in the container
WORKDIR /home/hauler

ARG HAULER_VERSION=1.0.1

# Download and extract hauler package
RUN apk add --no-cache tar unzip wget && \
    wget -q "https://github.com/rancherfederal/hauler/releases/download/v${HAULER_VERSION}/hauler_${HAULER_VERSION}_linux_amd64.tar.gz" && \
    tar xzf hauler_${HAULER_VERSION}_linux_amd64.tar.gz && \
    mv hauler /usr/local/bin/hauler && \
    rm -rf *

# Make hauler executable
RUN chmod +x /usr/local/bin/hauler

# Copy the entrypoint script to the container
COPY docker-entrypoint.sh /usr/local/bin/

# Set the entrypoint script to be executed
ENTRYPOINT ["docker-entrypoint.sh"]

# Expose the hauler registry port (5000)
EXPOSE 5000

# Expose the hauler fileserver port (8080)
# EXPOSE 8080

# By default, no command is necessary since it's handled in the entrypoint,
# but we can define a CMD as a default parameter or command to execute.
CMD []
