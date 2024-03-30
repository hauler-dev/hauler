FROM registry.suse.com/bci/golang:1.21 AS builder
RUN zypper --non-interactive install make bash wget ca-certificates

COPY . /build
WORKDIR /build
RUN make build

RUN echo "hauler:x:1001:1001::/home:" > /etc/passwd \
&& echo "hauler:x:1001:hauler" > /etc/group \
&& mkdir /store \
&& mkdir /fileserver \
&& mkdir /registry

FROM scratch
COPY --from=builder /var/lib/ca-certificates/ca-bundle.pem /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder --chown=hauler:hauler /home/. /home
COPY --from=builder --chown=hauler:hauler /tmp/. /tmp
COPY --from=builder --chown=hauler:hauler /store/. /store
COPY --from=builder --chown=hauler:hauler /registry/. /registry
COPY --from=builder --chown=hauler:hauler /fileserver/. /fileserver
COPY --from=builder --chown=hauler:hauler /build/bin/hauler /
USER hauler
ENTRYPOINT [ "/hauler" ]
