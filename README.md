# Description 
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

Testing: 
- [x] Inspect testing framework capability and implement some tests for the controller, derive goals from that => implemented tests for essential contractual behaviour with regards to ConfigMap syncing using the `Ginkgo` framework  (see notes.md `### Implemented Testcases`)
- [ ] add a usecase for the multitenancy capabiltiy to, (`envtest` allows to test RBAC?) Writing a test where both a serviceaccount with missing and sufficient privileges attempts to sync ConfigMaps across namespaces. 

Deployment:
- [ ] Deploy in cluster pod
- [ ] setup/install OLM (operator lifecycle manager) on dev k8s cluster (k3d, minikube, kind etc)
- [ ] Deploy as olm operator bundle

Security:
- [ ] Multitenancy: Can the operator be namespaced and run only in a subset of namespaces. Such that different users in the same cluster can't tamper with each others namespaces and ConfigMap?
  - [x] Research SAR (Subject Access review) suitability with practical use => Used for Authorization access review in Kubernetes. Can be used to query what actions a serviceaccount is allowed to do against the kube-apiserver API. Use `SubjectAccessReview` to co-locate a namespaced ConfigMapSync-Object with the serviceaccount on whose behalf configmaps will be synced across namespaces (I think Local SARs are unsuitable as we need to check authorizing actions for the target sync namespaces).
  - [x] implement namespaced `ConfigMapSync`
  - [x] implement watch on source/target ConfigMaps that are outside of ConfigMapSync's namespace! Look into watches with custom handlers, (something with `handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request` ... ). the watch shall be based on a label/annotation. See the GC label/annotation proposal => implemented using an annotation via which the event handler `handler.EnqueueRequestsFromMapFunc` then extracts from the annotation and enqueues a reconcile.requests for the name+namespace there in! Kubebuilder Watch-Event predicates might be used to also filter if the ConfigMap has these annotations, but I wonder if that is even more efficient. Predicates were instead used on Update-Events to filter only ConfigMaps to trigger reconciliation on changes to the `.Data` field of the ConfigMap
  - [x] Garbage Collection: When `ConfigMapSync` is namespaced, owernRef isn't possible. Need to implement GC via finalizer. For this track ownership via a label or annotation e.g. `configmapsync.owern=<namespace>/<name>` => Using the core kubernetes finalizer mechanic (a pre-deletion hook): Blocks deletion of CR till all target configmaps are at lesat attempted to be deleted, only then removes the finalizer. 
  - [ ] Document this ownership clearly for users
  - [x] Figure out if SAR is needed on each reconciliation or if token yielded via TokenRequest will always have the same RBAC as the associated SA. 
    - => Use SAR for better UX informing user via events or status.conditions why a given SA has insufficient rights! Use `TokenReuqest` to create a bounded token from a given serviceaccount (`.spec.serviceaccount`) on whose behalf reconciliation will take place for true multitenancy! I think using SARs for prevalidation to reissue TokenRequeset just-in-time is not necessary.
  - [x] allow specifying a ServiceAccount on whose behalf ConfigMap syncing will be allowed. => implemented by creating a scoped client using the given serviceaccount tokens identity. The identity can be provided on client creation in the form of a BearerToken (Extracted using `TokenRequest`).
  - [x] default/impute the ServiceAccount "default" => maybe this is a usecase for the WebhookServer! => WebhookServer wasn't necessary a kubebuilder codemarker above the field sufficed (This codemarker: `+kubebuilder:default:={name: "default"}`)
  - [ ] Cache Serviceaccount TokenRequest tokens using a thread-safe (because reconciliation can be concurrent) keyed map by `namespace/name` of the ConfigMapSync CR, as this will be unique, because it's namespaces. This can be apparently stored in the Reconcile struct as this is shared among the concurrent goroutines. Auto-renew on expiration (lazy, on-demand: so if not found => do TokenRequest and write in thread-safe map). Example implementation:
  - [ ] User Experience(UX) for ServiceAccount with missing RBAC rights => Use SAR to add a .status.Condition that indicates what RBAC rights a ServiceAccount is lacking to perform the ConfigMap syncing! Mostly implemented, currently only testing for one verb "update".

```go
func NewTokenCache() *TokenCache {
    return &TokenCache{
        tokens: make(map[string]tokenEntry),
    }
}

func (c *TokenCache) Get(key string) (string, bool) {
    c.mu.RLock()  // hopefully contention/lock is only per-key not whole map
    defer c.mu.RUnlock()

    entry, ok := c.tokens[key]
    if !ok  {
        return "", false // not present => Do TokenReview
    }
    return entry.token, true
}

func (c *TokenCache) Set(key, token string) {
    c.mu.Lock()
    defer c.mu.Unlock()

    // For huge clusters a goroutine could prune old key based on a expire timestamp that
    // we could store together with the token in the tokenEntry
    c.tokens[key] = tokenEntry{
        token:     token,
    }
}
```
- [ ] add .status.Condition for failing SARs; to clearly and "loudly" inform user if SA has insufficient rights, plus controller Logs and maybe even Events on the CR. Something like "SA X in namespace Y cannot create ConfigMap in namespace Z" 



Metrics
- [x] What metrics are available by default => have to start the main.go with a command line argument to automatically start the metrics server. For example (see the Makefile target `run-metrics`): `go run ./cmd/main.go -metrics-bind-address ":8080" --metrics-secure=false`
- [x] How can they be scraped => After the metrics server is strated by providing the cli flag, they can be scraped at the `/metrics` endpoint, as is custom with a prometheus metrics endpoint. So when listening on port :8080 locally scrape them like so `curl http://localhost:8080/metrics` 
- [x] What additional metrics can be exposed? Best practice? => The default provided metrics are quite extensive, I don't see an immediate reason to scrape custom ones 

Other Features:
- [ ] Analyze how the concept of Informers and workqueues impact Operator development. Is it just a performance feature?
- [ ] WebhookServer: Inspect usecases in operator development; Probably defaulting values and required fields
- [ ] Leader Election: How can it be added to the manager setup, what pros and cons does it provide (complexity increase?). Does it increase complexity of the reconciliation logic and how does it relate the controller setup option `MaxConcurrentReconciles`. I guess this is just for high-availability


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

