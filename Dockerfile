# BUILD STAGE
FROM golang:1.27.1 AS go-toolchain
FROM registry.access.redhat.com/ubi9/ubi AS builder

COPY --from=go-toolchain /usr/local/go /usr/local/go

ARG ARCH=amd64
ARG MAKE_TARGET=cross-build-linux-$ARCH

USER root

LABEL name=fetchit-build

ENV GOPATH=/opt/app-root GOCACHE=/mnt/cache GO111MODULE=on PATH=/usr/local/go/bin:$PATH

RUN dnf -y install gcc make git pkgconf-pkg-config gpgme-devel device-mapper-devel libseccomp-devel && dnf clean all

WORKDIR $GOPATH/src/github.com/containers/fetchit

COPY . .

RUN GOPATH=/opt/app-root GOCACHE=/mnt/cache make $MAKE_TARGET

RUN mv $GOPATH/src/github.com/containers/fetchit/_output/bin/linux_$ARCH/fetchit /usr/local/bin/

RUN mv ./scripts/entry.sh /usr/local/bin/

# RUN STAGE
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

RUN microdnf -y install rsync tar coreutils-single util-linux findutils grep sed device-mapper-libs libseccomp && command -v chroot && command -v flock && microdnf clean all

COPY --from=builder /usr/local/bin/fetchit /usr/local/bin/
COPY --from=builder /usr/local/bin/entry.sh /usr/local/bin/

WORKDIR /opt

CMD ["entry.sh"]
