# syntax=docker/dockerfile:1
# check=skip=InvalidDefaultArgInFrom

# ---- builder ----
FROM registry.suse.com/bci/golang:1.25.7 AS builder

ARG MK_HOST_ARCH
ENV ARCH=${MK_HOST_ARCH}

RUN zypper -n rm container-suseconnect && \
    zypper -n install git curl gzip tar wget awk

COPY --from=golangci/golangci-lint:v2.11.4-alpine@sha256:72bcd68512b4e27540dd3a778a1b7afd45759d8145cfb3c089f1d7af53e718e9 \
    /usr/bin/golangci-lint /usr/local/bin/golangci-lint

ENV HOME=/go/src/github.com/harvester/harvester-csi-driver
WORKDIR ${HOME}

# ---- base ----
FROM builder AS base
COPY . .

# ---- build ----
FROM base AS build
ARG MK_REPO_ID
RUN --mount=type=cache,id=harvester-csi-driver-go-mod-${MK_REPO_ID},target=/go/pkg/mod \
    --mount=type=cache,id=harvester-csi-driver-go-build-${MK_REPO_ID},target=/root/.cache/go-build \
    ./scripts/build

# ---- build-output ----
FROM scratch AS build-output
COPY --from=build /go/src/github.com/harvester/harvester-csi-driver/bin/ /

# ---- validate ----
FROM base AS validate
ARG MK_REPO_ID
RUN --mount=type=cache,id=harvester-csi-driver-go-mod-${MK_REPO_ID},target=/go/pkg/mod \
    --mount=type=cache,id=harvester-csi-driver-go-build-${MK_REPO_ID},target=/root/.cache/go-build \
    ./scripts/validate

# ---- validate-ci ----
FROM base AS validate-ci
ARG MK_REPO_ID
RUN git init -q && \
    git config user.email ci@harvester.local && \
    git config user.name ci && \
    git add -A && \
    git commit -q -m ci
RUN --mount=type=cache,id=harvester-csi-driver-go-mod-${MK_REPO_ID},target=/go/pkg/mod \
    --mount=type=cache,id=harvester-csi-driver-go-build-${MK_REPO_ID},target=/root/.cache/go-build \
    ./scripts/validate-ci

# ---- test ----
FROM base AS test
ARG MK_REPO_ID
RUN --mount=type=cache,id=harvester-csi-driver-go-mod-${MK_REPO_ID},target=/go/pkg/mod \
    --mount=type=cache,id=harvester-csi-driver-go-build-${MK_REPO_ID},target=/root/.cache/go-build \
    ./scripts/test
