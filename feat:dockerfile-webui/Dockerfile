# Build stage: Compile Go application
FROM dhi.io/golang:1-alpine3.21-dev AS builder

WORKDIR /build
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -a -ldflags="-s -w" -o hauler-ui main.go

# Obfuscator stage: Obfuscate JavaScript
FROM dhi.io/node:23-alpine3.21-dev AS obfuscator

RUN npm install -g javascript-obfuscator

WORKDIR /app
COPY frontend/ ./

RUN npx javascript-obfuscator app.js \
  --output app.obfuscated.js \
  --compact true \
  --control-flow-flattening true \
  --control-flow-flattening-threshold 0.75 \
  --dead-code-injection true \
  --dead-code-injection-threshold 0.4 \
  --string-array true \
  --string-array-threshold 0.75 \
  --string-array-encoding 'base64' \
  --unicode-escape-sequence false && \
  mv app.obfuscated.js app.js

# Hauler installer stage: Download hauler binary (requires curl + bash + openssl)
# Uses plain alpine for compatibility — this is a throwaway builder stage only.
FROM alpine:3.21 AS hauler-installer

RUN apk add --no-cache bash curl openssl && \
    curl -sfL https://get.hauler.dev | bash

# Pre-create data directories for the shell-less runtime stage
RUN mkdir -p /data/store /data/manifests /data/hauls /data/config

# Runtime stage: Minimal hardened image (no shell, no package manager)
FROM dhi.io/golang:1-alpine3.21

COPY --from=builder /build/hauler-ui /app/hauler-ui
COPY --from=obfuscator /app/ /app/frontend/
COPY --from=hauler-installer /usr/local/bin/hauler /usr/local/bin/hauler
COPY --from=hauler-installer /data/ /data/

ENV HAULER_STORE=/data/store

EXPOSE 8080 5000

CMD ["/app/hauler-ui"]
