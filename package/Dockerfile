# syntax=docker/dockerfile:1.7.0

FROM registry.suse.com/bci/bci-base:15.5

RUN zypper update --no-confirm && \
    zypper install --no-confirm curl iproute2 iputils nfs-client vim xfsprogs e2fsprogs zip

ARG TARGETPLATFORM

RUN if [ "$TARGETPLATFORM" != "linux/amd64" ] && [ "$TARGETPLATFORM" != "linux/arm64" ]; then \
    echo "Error: Unsupported TARGETPLATFORM: $TARGETPLATFORM" && \
    exit 1; \
    fi

ENV ARCH=${TARGETPLATFORM#linux/}

COPY bin/harvester-csi-driver-${ARCH} /usr/bin/harvester-csi-driver

ENTRYPOINT ["/usr/bin/harvester-csi-driver"]
