# Export shell defined to support Ubuntu
export SHELL := $(shell which bash)

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

# Include openshift build-machinery-go libraries
include ./vendor/github.com/openshift/build-machinery-go/make/golang.mk

SRC_ROOT :=$(shell pwd)

OUTPUT_DIR :=_output
CROSS_BUILD_BINDIR :=$(OUTPUT_DIR)/bin
CTR_CMD :=$(or $(shell which podman 2>/dev/null), $(shell which docker 2>/dev/null))
ARCH := $(shell uname -m |sed -e "s/x86_64/amd64/" |sed -e "s/aarch64/arm64/")

# restrict included verify-* targets to only process project files
GO_PACKAGES=$(go list ./cmd/... ./pkg/engine/...)

GO_LD_FLAGS := $(GC_FLAGS) -ldflags "-X k8s.io/component-base/version.gitMajor=0 \
                   -X k8s.io/component-base/version.gitMajor=0 \
                   -X k8s.io/component-base/version.gitMinor=0 \
                   -X k8s.io/component-base/version.gitVersion=v0.0.0 \
                   -X k8s.io/component-base/version.gitTreeState=clean \
                   -X k8s.io/client-go/pkg/version.gitMajor=0 \
                   -X k8s.io/client-go/pkg/version.gitMinor=0 \
                   -X k8s.io/client-go/pkg/version.gitVersion=v0.0.0 \
                   -X k8s.io/client-go/pkg/version.gitTreeState=clean \
                   $(LD_FLAGS)"

# These tags make sure we can statically link and avoid shared dependencies
GO_BUILD_FLAGS :=-tags 'include_gcs include_oss containers_image_openpgp gssapi providerless netgo osusergo exclude_graphdriver_btrfs'

# targets "all:" and "build:" defined in vendor/github.com/openshift/build-machinery-go/make/targets/golang/build.mk
fetchit: build-containerized-cross-build-linux-amd64
.PHONY: fetchit


OS := $(shell go env GOOS)
ARCH := $(shell go env GOARCH)

###############################
# host build targets          #
###############################

_build_local:
	@mkdir -p "$(CROSS_BUILD_BINDIR)/$(GOOS)_$(GOARCH)"
	+@GOOS=$(GOOS) GOARCH=$(GOARCH) $(MAKE) --no-print-directory build \
		GO_BUILD_PACKAGES:=./cmd/fetchit \
		GO_BUILD_BINDIR:=$(CROSS_BUILD_BINDIR)/$(GOOS)_$(GOARCH)

cross-build-linux-amd64:
	+$(MAKE) _build_local GOOS=linux GOARCH=amd64
.PHONY: cross-build-linux-amd64

cross-build-linux-arm64:
	+$(MAKE) _build_local GOOS=linux GOARCH=arm64
.PHONY: cross-build-linux-arm64

cross-build: cross-build-linux-amd64 cross-build-linux-arm64
.PHONY: cross-build

###############################
# containerized build targets #
###############################
_build_containerized_amd:
	@if [ -z '$(CTR_CMD)' ] ; then echo '!! ERROR: containerized builds require podman||docker CLI, none found $$PATH' >&2 && exit 1; fi
	$(CTR_CMD) build . --file Dockerfile --tag quay.io/fetchit/fetchit-amd:latest \
		--build-arg ARCH=amd64 \
		--build-arg MAKE_TARGET="cross-build-linux-amd64" \
		--platform="linux/amd64"

.PHONY: _build_containerized_amd

_build_containerized_arm:
	@if [ -z '$(CTR_CMD)' ] ; then echo '!! ERROR: containerized builds require podman||docker CLI, none found $$PATH' >&2 && exit 1; fi
	$(CTR_CMD) build . --file Dockerfile --tag quay.io/fetchit/fetchit-arm:latest \
		--build-arg ARCH=arm64 \
		--build-arg MAKE_TARGET="cross-build-linux-arm64" \
		--platform="linux/arm64"

.PHONY: _build_containerized_arm

build-containerized-cross-build-linux-amd64:
	+$(MAKE) _build_containerized_amd ARCH=amd64
.PHONY: build-containerized-cross-build-linux-amd64

build-containerized-cross-build-linux-arm64:
	+$(MAKE) _build_containerized_arm ARCH=arm64
.PHONY: build-containerized-cross-build-linux-arm64

build-containerized-cross-build:
	+$(MAKE) build-containerized-cross-build-linux-amd64
	+$(MAKE) build-containerized-cross-build-linux-arm64
.PHONY: build-containerized-cross-build

###############################
# ansible targets             #
###############################
_build_ansible_amd:
	@if [ -z '$(CTR_CMD)' ] ; then echo '!! ERROR: containerized builds require podman||docker CLI, none found $$PATH' >&2 && exit 1; fi
	$(CTR_CMD) build -f method_containers/ansible/Dockerfile --tag quay.io/fetchit/fetchit-ansible-amd:latest \
		--build-arg ARCH="amd64" \
		--build-arg MAKE_TARGET="cross-build-linux-amd64" \
		--platform="linux/amd64"

.PHONY: _build_ansible_amd

_build_ansible_arm:
	@if [ -z '$(CTR_CMD)' ] ; then echo '!! ERROR: containerized builds require podman||docker CLI, none found $$PATH' >&2 && exit 1; fi
	$(CTR_CMD) build -f method_containers/ansible/Dockerfile --tag quay.io/fetchit/fetchit-ansible-arm:latest \
		--build-arg ARCH="arm64" \
		--build-arg MAKE_TARGET="cross-build-linux-arm64" \
		--platform="linux/arm64"

.PHONY: _build_ansible_arm

build-ansible-cross-build-linux-amd64:
	+$(MAKE) _build_ansible_amd ARCH=amd64
.PHONY: build-ansible-cross-build-linux-amd64

build-ansible-cross-build-linux-arm64:
	+$(MAKE) _build_ansible_arm ARCH=arm64
.PHONY: build-ansible-cross-build-linux-arm64

build-ansible-cross-build:
	+$(MAKE) build-ansible-cross-build-linux-amd64
	+$(MAKE) build-ansbile-cross-build-linux-arm64
.PHONY: build-ansible-cross-build

###############################
#       systemd targets       #
###############################

systemd: build-systemd-cross-build-linux-amd64
.PHONY: systemd

_build_systemd_amd:
	@if [ -z '$(CTR_CMD)' ] ; then echo '!! ERROR: containerized builds require podman||docker CLI, none found $$PATH' >&2 && exit 1; fi
	$(CTR_CMD) build . --file method_containers/systemd/Dockerfile-systemctl --tag quay.io/fetchit/fetchit-systemd-amd:latest \
		--platform="linux/amd64"

.PHONY: _build_systemd_amd

_build_systemd_arm:
	@if [ -z '$(CTR_CMD)' ] ; then echo '!! ERROR: containerized builds require podman||docker CLI, none found $$PATH' >&2 && exit 1; fi
	$(CTR_CMD) build . --file method_containers/systemd/Dockerfile-systemctl --tag quay.io/fetchit/fetchit-systemd-arm:latest \
		--platform="linux/arm64"

.PHONY: _build_systemd_arm

build-systemd-cross-build-linux-amd64:
	+$(MAKE) _build_systemd_amd
.PHONY: build-systemd-cross-build-linux-amd64

build-systemd-cross-build-linux-arm64:
	+$(MAKE) _build_systemd_arm
.PHONY: build-systemd-cross-build-linux-arm64

build-systemd-cross-build:
	+$(MAKE) build-systemd-cross-build-linux-amd64
	+$(MAKE) build-systemd-cross-build-linux-arm64
.PHONY: build-systemd-cross-build

###############################
# dev targets                 #
###############################

clean-cross-build:
	$(RM) -r '$(CROSS_BUILD_BINDIR)'
	$(RM) -rf $(OUTPUT_DIR)/staging
	if [ -d '$(OUTPUT_DIR)' ]; then rmdir --ignore-fail-on-non-empty '$(OUTPUT_DIR)'; fi
.PHONY: clean-cross-build

clean: clean-cross-build
.PHONY: clean
