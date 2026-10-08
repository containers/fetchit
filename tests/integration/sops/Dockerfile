FROM quay.io/fedora/fedora:44
RUN dnf install -y podman crun age git && dnf clean all
# Go tests and the checksum-verified SOPS binary are mounted read-only by CI.
CMD ["/bin/bash"]
