# builder stage
FROM registry.suse.com/bci/bci-base:15.7 AS builder
ARG TARGETPLATFORM

# fetched from goreleaser build process
COPY $TARGETPLATFORM/hauler /hauler

RUN echo "hauler:x:1001:1001::/home/hauler:" > /etc/passwd \
&& echo "hauler:x:1001:hauler" > /etc/group \
&& mkdir /home/hauler \
&& mkdir /store \
&& mkdir /fileserver \
&& mkdir /registry

# release stage
FROM scratch AS release

COPY --from=builder /var/lib/ca-certificates/ca-bundle.pem /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder --chown=hauler:hauler /home/hauler/. /home/hauler
COPY --from=builder --chown=hauler:hauler /tmp/. /tmp
COPY --from=builder --chown=hauler:hauler /store/. /store
COPY --from=builder --chown=hauler:hauler /registry/. /registry
COPY --from=builder --chown=hauler:hauler /fileserver/. /fileserver
COPY --from=builder --chown=hauler:hauler /hauler /hauler

USER hauler
ENTRYPOINT [ "/hauler" ]

# debug stage
FROM alpine AS debug

COPY --from=builder /var/lib/ca-certificates/ca-bundle.pem /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder --chown=hauler:hauler /home/hauler/. /home/hauler
COPY --from=builder --chown=hauler:hauler /hauler /usr/local/bin/hauler

RUN apk --no-cache add curl

USER hauler
WORKDIR /home/hauler
