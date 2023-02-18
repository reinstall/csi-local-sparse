# Linux sparse file CSI Driver for Kubernetes
![build status](https://github.com/reinstall/csi-local-sparse/actions/workflows/ci.yml/badge.svg)

### About
This driver allows Kubernetes to provision local node volumes, csi plugin name:`csi-local-sparse.csi.reinstall.ru`. 
It supports dynamic provisioning of Persistent Volumes via Persistent Volume Claims by creating a new sparse image file
on the node.

### Project status: Alpha

### Container Images & Kubernetes Compatibility:
| Driver Version | supported k8s version |
|----------------|-----------------------|
| v0.1.0         | 1.20+                 |

### Install driver on a Kubernetes cluster
- install via [helm charts](./deployments/charts)

### Driver parameters
Set storageClassName with helm parameter `storageClass.name`

### Example

Install driver:
```
git clone https://github.com/reinstall/csi-local-sparse.git

helm install csi-local-sparse ./deployments/charts/csi-local-sparse --set "storageClass.name=local-sparse"
```

Create PersistentVolumeClaim:
```
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-pvc
spec:
  accessModes:
    - ReadWriteOnce
  volumeMode: Filesystem
  resources:
    requests:
      storage: 1Gi
  storageClassName: local-sparse
```