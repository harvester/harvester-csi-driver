FROM ubuntu:20.04

RUN apt-get update && apt-get install -y curl vim nfs-common iproute2 dnsutils iputils-ping zip xfsprogs

COPY bin/harvester-csi-driver /usr/bin/

ENTRYPOINT ["/usr/bin/harvester-csi-driver"]
