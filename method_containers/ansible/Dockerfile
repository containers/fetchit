FROM quay.io/fedora/fedora:35

ARG ARCH=$ARCH
ARG MAKE_TARGET=cross-build-linux-$ARCH-ansible

RUN dnf -y update && yum -y install openssh-clients ansible && yum clean all

ADD ansible.cfg /etc/ansible/
