## How to release a new version of harvester CSI driver

### Tag harvester CSI driver if needed. Sometimes, we could skip this part if only the support version was update on Rancher

  After Tagging, we need to update the following two parts: Rancher market (Also named Rancher Apps) and RKE2 build-in chart. Please see the following steps.

## Rancher Apps

Rancher Apps provides a convenient way to install applications on Rancher, like CIS benchmark (Security scan), Longhorn, Cloud provider, CSI driver, etc. We need to bump the Harvester CSI driver version for Rancher Apps so that users can use the latest CSI driver for Harvester. The detailed steps are as follows:
### Bump new version for Rancher apps

  Harvester CSI driver provides a standard CSI interface used by guest Kubernetes clusters on Rancher with Harvester.
  It is a [standalone helm chart package](https://github.com/rancher/charts/tree/dev-v2.8/charts/harvester-csi-driver). We could install it as a Rancher app directly.
  So, we need to bump the new harvester CSI version for Rancher. Then, users could install the latest version and its features.
 
  1. For Rancher apps, you need to update the following files:
  - packages/harvester/harvester-csi-driver/generated-changes/patch/Chart.yaml.patch - update the needed info (like kube-version, rancher-version, upstream-version, appVersion, etc.)
  - kube-version: means the kubernetes version that the app supports.
  - rancher-version: means the rancher version that the app supports.
  - upstream-version: means the upstream version (The version of this app).
  - appVersion: means the real version of the harvester csi driver (refer to release version).
  - packages/harvester/harvester-csi-driver/package.yaml - update the version. you could refer [here](https://github.com/rancher/charts#versioning-charts) to know how to fill it correctly.
  - release.yaml - add the new version to the release.yaml file.

  2. After updating the above files, you need to execute `make prepare`, `make patch` and `make clean`. Remember you could add variable PACKAGE=`harvester/harvester-csi-driver` to speed up this process.
  
  3. Commit the changes until now.
  
  4. Use `PACKAGE=harvester/harvester-csi-driver make charts` to generate the new charts files.
  
  5. Commit these changes for the new chart.
  
  6. Run `make validate` to make sure the CI would be passed.

  7. Reference PR:
  - [Bump harvester-csi-driver to v0.1.16 and harvester-cloud-provider to v0.1.14](https://github.com/rancher/charts/pull/2450)

## RKE2 build-in chart

  RKE2 has some build-in charts shipped with it. Harvester CSI driver is one of them. We need to bump the new version for RKE2 build-in chart.
### Bump new version for Rancher RKE2 chart

 1. Update the following files:
 - packages/harvester-csi-driver/package.yaml - update the version to the latest version (This version should be consistent with harvester [charts](https://github.com/harvester/charts/releases)).
  
 2. Reference PR:
 - [Bump Harvester-CSI-driver v0.1.16](https://github.com/rancher/rke2-charts/pull/326)


### Bump new version for Rancher RKE2

   1. Update the following files:
   - The `CHART_VERSION` will be a minor difference from the previous. RKE2 would regenerate the package with its version. Please refer to [here](https://github.com/rancher/rke2-charts/blob/main-source/packages/harvester-csi-driver/package.yaml#L2C1-L2C15). The actual version of RKE2 chart would be `0.1.1600`. The last `00` is the packageVersion of RKE2.
   - Dockerfile - you can find the `CHART_VERSION` of harvester-csi-driver and update its latest version. (Please make sure the version is consistent with the above) 
   - scripts/build-images - fill the harvester-csi-driver release version for build images.

   2. Reference PR:
   - [Bump harvester csi driver to v0.1.16](https://github.com/rancher/rke2/pull/3999)
