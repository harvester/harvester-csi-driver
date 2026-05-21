# syntax=docker/dockerfile:1.7.0

FROM registry.suse.com/bci/bci-base:16.0

# util-linux: for mount related commands
RUN zypper update --no-confirm && \
    zypper install --no-confirm util-linux curl iproute2 iputils nfs-client vim xfsprogs e2fsprogs zip

ARG TARGETPLATFORM

RUN if [ "$TARGETPLATFORM" != "linux/amd64" ] && [ "$TARGETPLATFORM" != "linux/arm64" ]; then \
    echo "Error: Unsupported TARGETPLATFORM: $TARGETPLATFORM" && \
    exit 1; \
    fi

ENV ARCH=${TARGETPLATFORM#linux/}

COPY --from=bin harvester-csi-driver-${ARCH} /usr/bin/harvester-csi-driver

ENTRYPOINT ["/usr/bin/harvester-csi-driver"]
