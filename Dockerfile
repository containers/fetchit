FROM docker.io/fedora:35 AS harpoon-builder
ENV APP_ROOT=/opt/app-root
RUN dnf -y install golang gpgme-devel libbtrfs btrfs-progs-devel device-mapper-devel
RUN mkdir -p $APP_ROOT/src/github.com/redhat-et/engine
ADD engine/ $APP_ROOT/src/github.com/redhat-et/harpoon/engine
WORKDIR $APP_ROOT/src/github.com/redhat-et/harpoon/engine 
RUN go build . 

FROM registry.access.redhat.com/ubi8/ubi

RUN yum -y update && yum clean all && rm -rf /var/cache/yum

COPY --from=harpoon-builder /opt/app-root/src/github.com/redhat-et/harpoon/engine /usr/local/bin
