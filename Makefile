export DOCKER_BUILDKIT := 1

ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))

MK_REPO_ID := $(shell printf '%s' "$(ROOT)$$(cat /etc/machine-id 2>/dev/null || echo none)" | shasum -a 256 | cut -c1-12)
MK_HOST_ARCH := $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')
MK_DOCKER_PROGRESS ?= plain
MK_DOCKER_RUN_OPTS_TTY := $(if $(CI),,-it)

ifeq ($(CI),)
BOLD  := \033[1m
CYAN  := \033[36m
RESET := \033[0m
else
BOLD  :=
CYAN  :=
RESET :=
endif

BANNER = @printf "$(BOLD)$(CYAN)[target: $@]$(RESET)\n"

DOCKER_BUILD := docker build \
	--progress=$(MK_DOCKER_PROGRESS) \
	--build-arg MK_REPO_ID=$(MK_REPO_ID) \
	--build-arg MK_HOST_ARCH=$(MK_HOST_ARCH) \
	-f $(ROOT)/Dockerfile $(ROOT)

.DEFAULT_GOAL := ci

.PHONY: ci build test validate validate-ci package gen-version-env clean help

ci: build test validate validate-ci package
	$(BANNER)

gen-version-env:
	$(BANNER)
	@./scripts/version >/dev/null

build: gen-version-env
	$(BANNER)
	$(DOCKER_BUILD) --target build-output --output type=local,dest=$(ROOT)/bin

test: gen-version-env
	$(BANNER)
	$(DOCKER_BUILD) --target test

validate: gen-version-env
	$(BANNER)
	$(DOCKER_BUILD) --target validate

validate-ci: gen-version-env
	$(BANNER)
	$(DOCKER_BUILD) --target validate-ci

package: gen-version-env build
	$(BANNER)
	@./scripts/package

clean:
	$(BANNER)
	rm -rf $(ROOT)/bin $(ROOT)/scripts/.version_env

help:
	@echo "Targets:"
	@echo "  build         - cross-compile binaries into bin/"
	@echo "  test          - run go test"
	@echo "  validate      - run golangci-lint and go fmt check"
	@echo "  validate-ci   - run go generate and verify the tree is clean"
	@echo "  package       - build the runtime container image (host docker)"
	@echo "  ci            - build + test + validate + validate-ci + package"
	@echo "  clean         - remove bin/ and scripts/.version_env"
