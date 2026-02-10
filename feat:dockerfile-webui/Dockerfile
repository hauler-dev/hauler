FROM golang:1.21-alpine AS builder

WORKDIR /build
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -o hauler-ui main.go

FROM node:18-alpine AS obfuscator

RUN npm install -g javascript-obfuscator

WORKDIR /app
COPY frontend/ ./

RUN javascript-obfuscator app.js \
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

FROM alpine:latest

RUN apk add --no-cache ca-certificates curl bash openssl

RUN curl -sfL https://get.hauler.dev | bash

WORKDIR /app
COPY --from=builder /build/hauler-ui /app/
COPY --from=obfuscator /app/ /app/frontend/

RUN mkdir -p /data/store /data/manifests /data/hauls /data/config

ENV HAULER_STORE=/data/store

EXPOSE 8080 5000

CMD ["/app/hauler-ui"]
