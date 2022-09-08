# BUILD STAGE
FROM registry.access.redhat.com/ubi8/go-toolset as builder

ARG ARCH=amd64
ARG MAKE_TARGET=cross-build-linux-$ARCH

USER root

LABEL name=fetchit-build

ENV GOPATH=/opt/app-root GOCACHE=/mnt/cache GO111MODULE=on

WORKDIR $GOPATH/src/github.com/containers/fetchit

COPY . .

RUN dnf -y install gpgme-devel device-mapper-devel

RUN GOPATH=/opt/app-root GOCACHE=/mnt/cache make $MAKE_TARGET

RUN mv $GOPATH/src/github.com/containers/fetchit/_output/bin/linux_$ARCH/fetchit /usr/local/bin/

# RUN STAGE
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

RUN microdnf update && microdnf install --nodocs rsync && microdnf clean all

COPY --from=builder /usr/local/bin/fetchit /usr/local/bin/

WORKDIR /opt

CMD ["/usr/local/bin/fetchit", "start"]
