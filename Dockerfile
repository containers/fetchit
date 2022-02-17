# BUILD STAGE
FROM registry.access.redhat.com/ubi8/go-toolset as builder

ARG ARCH=amd64
ARG MAKE_TARGET=cross-build-linux-$ARCH

USER root

LABEL name=harpoon-build

ENV GOPATH=/opt/app-root GOCACHE=/mnt/cache GO111MODULE=on

WORKDIR $GOPATH/src/github.com/redhat-et/harpoon

COPY . .

RUN dnf -y install gpgme-devel device-mapper-devel

RUN GOPATH=/opt/app-root GOCACHE=/mnt/cache make $MAKE_TARGET

RUN mv $GOPATH/src/github.com/redhat-et/harpoon/_output/bin/linux_$ARCH/harpoon /usr/local/bin/

# RUN STAGE
FROM registry.access.redhat.com/ubi9-beta/ubi:latest

COPY --from=builder /usr/local/bin/harpoon /usr/local/bin/

WORKDIR /opt

CMD ["/usr/local/bin/harpoon"]
