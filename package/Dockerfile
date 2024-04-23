FROM registry.suse.com/bci/bci-base:15.5

RUN zypper update --no-confirm && \
    zypper install --no-confirm curl iproute2 iputils nfs-client vim xfsprogs e2fsprogs zip

COPY bin/harvester-csi-driver /usr/bin/

ENTRYPOINT ["/usr/bin/harvester-csi-driver"]
