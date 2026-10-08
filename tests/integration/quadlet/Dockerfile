FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
RUN microdnf install -y coreutils-single util-linux findutils grep sed tar && command -v chroot && command -v flock && microdnf clean all
