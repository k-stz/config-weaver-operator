# config-weaver-operator
The `config-weaver-operator` simplifies configuration management by automatically syncing ConfigMaps and Secrets across namespaces. Whether you need to distribute a `PullSecrets`, `CA` certificates, Trust Bundles, or other shared resources, this operator ensures consistency and reliability.

## Goals
- [x] Generate ConfigMaps accross namespaces (no content yet, just by name), use cluster-scoped CR for easier implementation 
- [x] Ensure ConfigMaps are kept in sync on change => implemented via ownerReference Watch on clusterscoped CR
- [x] Sync whole content of ConfigMaps
- [x] Ensure endless reconcile loops don't occur on .spec.conditions append; => fixed by using library function `meta.SetStatusCondition` which gives set-like properties and orders all conditions deterministically and only changes timestamps conditions status changed => thus avoiding unnecessary event when applying 
- [x] sync ConfigMap content as well with given source ConfigMap
- [x] Ensure .Status is rebuild on each reconcilation when needed => not complete but implemented by tracking the state of the reconciliation in the reciver struct's field ConfigMapSyncReconciler.RunState. And at the end of the Reconcile()-call the conditions are built
- [x] Implement ObservedGeneration => comes with meta.SetStatusCondition
- [ ] Implement timebased reconcilation trigger for robustness (when missed event "edges" in the signalig due to, for example, network partition or noisy neighbor) => implement this via the Reoncile() returned `Result{RequeueAfter: <time.Duration>}` struct
- [x] implement ownership and cascading deletion: deleting ConfigMapSync deletes all configmaps => via ownerReference on cluster-scoped CR
- [x] .Status: imlement tracking significant state transitions with .status.Conditions; implement at least the "Ready" state
- [ ] Log levels: ensure logs for state transitions use verbosity level also matches the details level. For example r.Update() should be logged at low verbosity but entering a function at high verbosity
- [ ] Inspect when DeepCopy() is necessary: For example when concurrent Reconciliation take place and concurrent process A executes an r.Get() fetching the object state from the K8s API and then an r.Updates() does it get written to a cache shared between other concurrent goroutines? Such that if process B executes an r.Get() does it feath instead process A's altered memory from the cache, leading to an unintendet state?
- [ ] Testing: Inspect testing framework capability and implement some tests for the controller, derive goals from that

Deployment:
- [ ] Deploy in cluster pod
- [ ] setup/install OLM (operator lifecycle manager) on dev k8s cluster (k3d, minikube, kind etc)
- [ ] Deploy as olm operator bundle

Security:
- [ ] Multitenancy: Can the operator be namespaced and run only in a subset of namespaces. Such that differnt users in the same cluster can't tamper with each otherss namespaces and configmaps?


Metrics
- [ ] What metrics are available by default
- [ ] How can they be scraped
- [ ] What additional metrics can be exposed? Best practice?

Other Features:
- [ ] Analyze how the concept of Informers and workqueues impact Operator development. Is it just a performance feature?
- [ ] WebhookServer: Inspect usecases in operator development; Probably defaulting values and required fields
- [ ] Leader Election: How can it be added to the manager setup, what pros and cons does it provide (complexity increase?). Does it increase complexity of the reconciliation logic and how does it relate the controller setup option `MaxConcurrentReconciles`. I guess this is just for high-availability


## Description
// TODO(user): An in-depth paragraph about your project and overview of use


## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/config-weaver-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/config-weaver-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/config-weaver-operator:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/config-weaver-operator/<tag or branch>/dist/install.yaml
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

