FROM golang:1.21-alpine AS builder
RUN apk add make bash
RUN apk add --no-cache ca-certificates

COPY . /build
WORKDIR /build
RUN make build

RUN echo "hauler:x:1001:1001::/home:" > /etc/passwd \
&& echo "hauler:x:1001:hauler" > /etc/group \
&& mkdir /store \
&& mkdir /store-files \
&& mkdir /registry

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder --chown=hauler:hauler /home/. /home
COPY --from=builder --chown=hauler:hauler /tmp/. /tmp
COPY --from=builder --chown=hauler:hauler /store/. /store
COPY --from=builder --chown=hauler:hauler /registry/. /registry
COPY --from=builder --chown=hauler:hauler /store-files/. /store-files
COPY --from=builder --chown=hauler:hauler /build/bin/hauler /
USER hauler
ENTRYPOINT [ "/hauler" ]
