# gmos-k8s-operator
// TODO(user): Add simple overview of use/purpose

## Description
// TODO(user): An in-depth paragraph about your project and overview of use

## Getting Started

### Prerequisites
- go version v1.20.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### Run automated tests

Before attempting to deploy to a cluster, ensure automated tests pass.

```
make test
```

### (Recommended) Install a local kubernetes development environment with minikube

See [minikube start](https://minikube.sigs.k8s.io/docs/start/) documentation
for installation installation instructions. After installation, ensure you are running
kubectl against your local cluster with:

```
kubectl config use-context minikube
```


### To Deploy on the cluster
**Build and your image into the minikube docker namespace**

```sh
# minikube build env doesn't appear to support docker-compose, need to run a raw docker build command
eval $(minikube docker-env)
docker build -t hub.opensciencegrid.org/mwestphall/gmos-k8s-operator .
```

**Create an OSPool access token Secret:**

For testing purposes, this may be a dummy value. If you wish for deployed EPs to connect to a real
htcondor pool, you must supply a real IDToken for that pool.

```sh
kubectl create secret generic \
  --dry-run=client gm-file-server-pilot-tokn \
  --from-literal=ospool-ep.tkn=<some token value> -o yaml \
  > kustomize/secrets/pilot-token2.yaml 
```

> **WARNING:** this token file is .gitignored by default. If using a real token, ensure that that it is not committed.


**Deploy to the local minikube cluster**:

```sh
kubectl apply -k kustomize
```

### To inspect a running instance of the operator

Tail the logs of the operator pod in the operator namespace:

```sh
kubectl -n glidein-manager-operator logs -f deployment/glidein-manager-controller-manager
```

Inspect the resources created in the `dev` namespace by the operator:

- Deployments:
  ```sh
  kubectl -n dev get deployment | grep glideinmanagerpilotset
  ```
- Pods:
  ```sh
  kubectl -n dev get pod | grep glideinmanagerpilotset
  ```
- ConfigMaps:
  ```sh
  kubectl -n dev get configmap | grep glideinmanagerpilotset
  ```
- Secrets:
  ```sh
  kubectl -n dev get secret | grep glideinmanagerpilotset
  ```


Update the Custom Resource (CR) that drives the operator's behavior, observe
the operator's response to changes
```sh
kubectl -n dev edit glideinmanagerpilotset glideinmanagerpilotset-sample
kubectl -n glidein-manager-operator logs -f deployment/glidein-manager-controller-manager
```

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
./delete_resources.sh
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

