FROM index.docker.io/fedora:35

ARG ARCH=amd64
ARG MAKE_TARGET=cross-build-linux-$ARCH-ansible

RUN yum -y install openssh-clients ansible && yum clean all

ADD ansible.cfg /etc/ansible/
