FROM registry.suse.com/bci/bci-base:15.4

RUN zypper update --no-confirm && \
    zypper install --no-confirm curl iproute2 iputils nfs-client vim xfsprogs zip

COPY bin/harvester-csi-driver /usr/bin/

ENTRYPOINT ["/usr/bin/harvester-csi-driver"]
