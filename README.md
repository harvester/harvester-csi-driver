# Harvester CSI Driver
test

The Harvester Container Storage Interface (CSI) Driver provides a [CSI](https://github.com/container-storage-interface/spec/blob/master/spec.md) interface used by guest kubernetes clusters in Harvester. It connects to the host cluster and hot-plug host volumes to the VMs to provide native storage performance.

## Building

`make`


## Deploying

### Prerequisites

- The Kubernetes cluster is built on top of Harvester virtual machines.
- The Harvester virtual machines running as guest Kubernetes nodes are in the same namespace.

### Deploying with Harvester RKE2 node driver

When spin up a kubernetes cluster using Rancher RKE2 node driver, the Harvester CSI driver will be deployed when harvester cloud provider is selected.

### Deploying with Harvester RKE1 node driver

1. Select the external cloud provider option.

2. Generate addon configuration and add it in the rke config yaml.

```
# depend on kubectl to operate the Harvester
./deploy/generate_addon.sh <serviceaccount name> <namespace>
```

## License
Copyright (c) 2023 [Rancher Labs, Inc.](http://rancher.com)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
