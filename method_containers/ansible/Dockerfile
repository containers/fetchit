FROM quay.io/fedora/fedora:36

ARG ARCH=$ARCH
ARG MAKE_TARGET=cross-build-linux-$ARCH-ansible

RUN yum -y install openssh-clients ansible && yum clean all

ADD ansible.cfg /etc/ansible/
