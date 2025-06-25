# Setup `k3d`
In order to connect to the cluster from a remote machine, we need to
set in the cert the san.

With the following we create the cluster that will in its SAN for the api server servce, among other defaults, the hostname "raspik3d". So make sure in your `/etc/hosts` to resolve it to this clusters ip.
```sh
k3d cluster create mycluster --agents 1 -p "9000:80@loadbalancer" -p "8443:443@loadbalancer" --registry-use k3d-myregistry --k3s-arg "--tls-san=raspik3d@server:0"
```
The syntax `@server:0` is a nodefilter in k3d jargon. It will refer to the
nodes (list them with `k3d node list`) with the name `k3d-mycluster-server-0`
which has the role "server" and will host the kube-apiserver.


Push images to docker local k3d image registry
```sh
skopeo copy --dest-tls-verify=false docker-daemon:app_flask:latest docker://localhost:43761/app_flask:latest
# docker-daemon will refer to locally running docker registry server
# --dset-tls-verify=flase is necessary lest the k3d registry will throw https errors
```

Debug network services from inside the cluster. For example
inspect internal registry
```sh
# Image containing curl
kubectl run test --rm -it --image=curlimages/curl -- sh
# check registry content from inside the cluster (by default inside the docker network it is mapped to port 5000, outside to 43761):
curl k3d-myregistry:5000/v2/_catalog
{"repositories":["app_flask"]}
```

Then list the tags of a specific image
```sh
curl k3d-myregistry:5000/v2/app_flask/tags/list
{"name":"app_flask","tags":["latest"]}
```
`k3d` links the reigstry at the Docker network level, not as a Kubernetes Service. So you won't find it via `kubectl get services -A`

## Raspi
### Problem: Hanging k3d-mycluster-server-0:
Problems starting k3d, hanging on `Starting node 'k3d-mycluster-server-0'`. Resolved by adding to `cgroup_enable=memory` to /boot/cmdline.txt and restarting the raspi: 
```sh
root@raspberrypi:/boot# cat /boot/cmdline.txt 
console=serial0,115200 console=tty1 root=/dev/mmcblk0p7 rootfstype=ext4 elevator=deadline fsck.repair=yes rootwait quiet splash plymouth.ignore-serial-consoles cgroup_enable=memory
```

After launching k3d on raspi, you can connect to it by fetching the kubeconfig.
`k3d kubeconfig`

## Problem: x509
After saving the kubeconfig to the config of the machine you want to use to connect to the raspi with the following:
```sh
ssh user@k3draspi sudo k3d kubeconfig get mycluster > ~/.kube/config
```
When you attempt to use it:
```sh
$ kubectl get pods
E0324 00:11:02.015114  708640 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://0.0.0.0:34067/api?timeout=32s\": dial tcp 0.0.0.0:34067: connect: connection refused"
The connection to the server 0.0.0.0:34067 was refused - did you specify the right host or port?
```
Ok so you update the kubeconfig to match the ip of the remote for example
`sed -i s/0.0.0.0/raspik3d/g ~/.kube/config`

Then when you run again you get an tls error:
```sh
kubectl get pods
...
E0324 00:18:40.972066  709578 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: Get \"https://raspi:34067/api?timeout=32s\": tls: failed to verify certificate: x509: certificate is valid for k3d-mycluster-server-0, k3d-mycluster-serverlb, kubernetes, kubernetes.default, kubernetes.default.svc, kubernetes.default.svc.cluster.local, localhost, not raspi"
Unable to connect to the server: tls: failed to verify certificate: x509: certificate is valid for k3d-mycluster-server-0, k3d-mycluster-serverlb, kubernetes, kubernetes.default, kubernetes.default.svc, kubernetes.default.svc.cluster.local, localhost, not raspi
```
Which means that though we do trust the certificate, it's in the kubeconfig, we failed to verify it. The verification failed because the cert is
valid for a different SAN (`Subject Alternative Name`) than what we're using and it lists the SANs. 

You can fix this in three ways:
- cli ignore: `kubectl get pods --insecure-skip-tls-verify` 
- kubeconfig (not production use):
  under clusters set:
```yaml
clusters:
- cluster:
  # you have to certifcate-authority-data: ... for it to work, else
  # kubectl will throw "error: specifing a root certificate file with insecure flag is not allowed..."
  server: https://raspi:36809
  insecure-skip-tls-verify: true
```
- Or we can simply add one of the SANs on the certificate to `/etc/hosts` and use that as the server in the kubeconfig like `k3d-mycluster-server-0`:
```file
<ip-k3d-hosting-machine> k3d-mycluster-server-0
```
And update it in the clusters server entry in the kubeconfig:
`sed -i ~/.kube/config s/0.0.0.0/k3d-mycluster-server-0/g`

Finally, it might be possible to adjust the certificate SANs by creating the k3d cluster by passing a `--config`, there we provide a Kind-Yaml
that under options.k3s.extraArgs allows to pass tls-san:
```yaml
  k3s: # options passed on to K3s itself
    extraArgs: # additional arguments passed to the `k3s server|agent` command; same as `--k3s-arg`
      - arg: --tls-san=my.host.domain  #<- here
        nodeFilters:
          - server:*
```

# Deploy pod using image from internal registry
Finally run the newly pushed image in k3d cluster, note that you always have to provide the registry, else docker.io is implied and it will not find it:
```sh
kubectl run app-flask --image=k3d-myregistry:43761/app_flask:latest
# or using the docker network internal port, both work:
kubectl run app-flask --image=k3d-myregistry:5000/app_flask:latest
# Expose it via service
kubectl expose pod app-flask --port 80 
```

Configure the traefik ingress-controller to loadbalance to the app-flask
service on port :80
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: app-flask
  annotations:
    ingress.kubernetes.io/ssl-redirect: "false"
spec:
  rules:
  - http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: app-flask
            port:
              number: 80
```

## Other
Provide hostpath as pvc in k3d cluster:
```sh
# provides host path in pvc under /data
k3d cluster create my-cluster --volume /my/home/path/to/some/dir:/data
```

Load local image into k3d:
```sh
# Apparently those get loaded on the host nodes, not in the k3d registry. The registry has to be cretad separetly and included in the cluster (k3d registry create)
k3d image import <imagename> -c my-cluster
```

## `k9s` instead of kubernetes dashboard
Use `k9s` instead off the kubernetes dashboard. It's way more lightweight and doesn't need to be run in the cluster.

# Operator-SDK Workflow
1. Create Project:
```
operator-sdk init --domain example.com --repo github.com/k-stz/config-weaver-operator
```
Create an APIGroup and a first Resource:
```
operator-sdk create api --group weaver --version v1alpha1 --kind ConfigMapSync --resource --controller
```


The resulting APIGroup  will be what you pass as --group in kubebuilder `create api` plus what you set as --domain in `operator-sdk init --domain`.
So in this case will be `weaver.example.com`

# Orphan cascading deletion
The orphan deletion kubectl syntax is astonishingly picky:

```sh
# works:
kubectl delete configmapsyncs.weaver.example.com configmapsync-sample --cascade=orphan
# WRONG - doesn't work:
# In this case the --cascade=orphan must use an equal sign for the option!
kubectl delete configmapsyncs.weaver.example.com configmapsync-sample --cascade orphan
```

# Switching CRD from namespaced to clusterscoped
For an easier initial implementation ConfigMapSync will be switched from namespaced to cluster scope. This is achieved by simple setting the codemarker `+kubebuilder:resource:scope=Cluster` above the API type definition and run `make manifests`. To combine it with shortnaming, you need to write it in one:
`// +kubebuilder:resource:scope=Cluster,path=configmapsyncs,shortName=cms;cmsync`
Else when you write it in separate lines, only one entry will be considered

But after you apply the new CRD to the cluster, what happens to existing namespaced `ConfigMapSync` object in the cluster? The apply will protest! See:
```sh
$ make install
/home/k-stz/code/config-weaver-operator/bin/controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
/home/k-stz/code/config-weaver-operator/bin/kustomize build config/crd | kubectl apply -f -
The CustomResourceDefinition "configmapsyncs.weaver.example.com" is invalid: spec.scope: Invalid value: "Cluster": field is immutable
make: *** [Makefile:182: install] Error 1
```

So we need to delete the old CRD, thereby removing all its CR and thus solving thus conflict.

# Design Notes
## Multitenancy Design: Problem / Challenge
We need:
- A custom resource (e.g. ConfigMapSync) that lets a user define "sync this ConfigMap from namespace-A to namespace-B, namespace-C, etc" implemented via an operator (controller) that does the actual syncing.
- Multitenancy: users should only be able to sync between namespaces they have access to (e.g. RBAC allows them to create/update ConfigMaps in those namespaces).
- The operator runs with elevated permissions (as most do) but should act only on behalf of the requesting user, within their access scope.

The Challenge:
Kubernetes controllers run as cluster components with service accounts that usually have broad access. So by default, the operator can sync ConfigMaps regardless of who created the `ConfigMapSync`` object. That’s the problem. 

Approach:
Query the users permission on creation of `ConfigMapSync` objects and embed those rights declaratively in the ConfigMapSync field? Then from then on only those namespaces are syncable...? 

### Solution Proposal
Inspired by the openshift logging operator which for the resource `ClusterLogForwarder` demands that you supply a serviceaccount which has rights to  scraped/forwarded logs. We might do the same.

The idea is to:
1. in the ConfigMapSync CR will be namespaced
2. It includes a ServiceAccount field where the user may only reference a SA in the same namespace
3. The operator:
 - reads the CR
 - uses the provided SA to impersonate or use its token
 - uses that identity to perform the actual syncing (e.g., create/update ConfigMaps in target namespace)
 4. Thus the ability to sync is bound by the RBAC rules attached to that SA, not the operator's own prvileges
 
### Evaluation of Solution

### Pros
- **multitenancy safe**: the operator is just a "conduit" and enforces actions only within the bounds of what the user's `ServiceAccount` is allowed to do!
- **RBAC-native**: Admins already know how to mange RBAC roles - we're reusing a familiar permission model
- **Namespace-isolated**: users can't smuggle in another namespace's SA (will disallow referencing one from another namespace)
- **Scalable**: don't need to issue SARs for every action (less API traffic and code complexity)

### Caveates / Refinements**:
Token Mounting:
- Token Mounting and Access: After accessing the token for that SA operators can create a token for the SA using `TokenRequest API` => Then in the controller code we might do client-requests to the kube-apiserver using the token of a given SA!
- Operator RBAC update: this requires the fllowing permissions
``` yaml
apiGroups: ["authentication.k8s.io"]
resources: ["serviceaccounts/token"]
verbs: ["create"]
```
- token should be short-lived and scoped to a minimal audience
  - scope: for example be bound to the life extend of the controller Pod, or to the `ConfigMapSync-Object`. This means in the latter case, that the token would become invalid the moment the ConfigMapSync object were to be deleted. 

Impersonation vs Token Use Tradeoffs
- 🔄 Impersonation:
  - You impersonate the user or SA by adding headers (Impersonate-User, Impersonate-Group)	Requires impersonate RBAC permission 
  - powerful and risky if misconfigured
- 🔑 TokenRequest (recommended): 
  - You get an actual token and create a new REST client with it	
  - Cleaner and easier to audit
  - Usability: If no ServiceAccount is provided the current namespace's `default`-ServiceAccount is imputed => via WebServer defaulting!?

### Complication: ownerReference for namespaced Resource 
To ease the multitenant implementation, namespaced CR (`ConfigMapSync`) is preferred. But the created ConfigMaps can't then set the ownerReference, as this is forbidden to point to a namespaced owner.
What do we need the ownerReference for:
- easy GC: when deleting CR all childs dependents will also be deleted
- easy dependent watch: the controller-runtime framework has a convenient watch implementation for owned objects. Now that it's missing, we'd need to implement a mechanism to watch changes on all the ConfigMaps and figure out the namespaced `ConifgMapSync` that owns them.

Possible Solutions:
- Index + Watch with "Predicate Filters": 
  - The configmaps will be labeld with a owner-namespace and owner-name.
  - The ConfigMapSync CRs can be indexed! See "mgr.GetFieldIndexer"...
  - watch ConfigMaps with a custom event handler, instead of .Owns() use .Watches()... effectively triggering the reconciliation handler for the `ConfigMapSync`` controller whenever a ConfigMap is updated with the labels set!
  - finalizers: to get GC back in, we can implement logic on the /finalizer subresource!

### Analyzing cluster-logging-forwarder operator implementation
repo: ` https://github.com/openshift/cluster-logging-operator`

Uses `ReconcileServiceAccountTokenSecret()` that creates a service-account-token secret for a given ServiceAccount. This injects a token key that contains a long-lived token into the secrets, this is a kubernetes feature. 
```go
// cluster-logging-operator/internal/auth/service_account.go
func ReconcileServiceAccountTokenSecret(sa *corev1.ServiceAccount, k8sClient client.Client, namespace, name string, owner metav1.OwnerReference) (desired *corev1.Secret, err error) {

	if sa == nil {
		return nil, fmt.Errorf("serviceAccount not provided in-order to generate a token")
	}

	desired = runtime.NewSecret(namespace, name, map[string][]byte{})
	desired.Annotations = map[string]string{
		corev1.ServiceAccountNameKey: sa.Name,
		corev1.ServiceAccountUIDKey:  string(sa.UID),
	}
	desired.Type = corev1.SecretTypeServiceAccountToken
	utils.AddOwnerRefToObject(desired, owner)
	current := &corev1.Secret{}
	if err = k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(desired), current); err == nil {
		return current, nil
	} else if !errors.IsNotFound(err) {
		return nil, fmt.Errorf("failed to get %s token secret: %w", name, err)
	}

	if err := k8sClient.Create(context.TODO(), desired); err != nil {
		return nil, fmt.Errorf("failed to create %s token secret: %w", name, err)
	}

	return desired, nil
}

```
Where is this called? In the `cluster-logging-operator/internal/controller/observability/collector.go` 


## ConfigMapSync: Conditations
in `.status.conditions` a slice of metav1.Condition is stored.
``` go
//	    // Known .status.conditions.type are: "Available", "Progressing", and "Degraded"
```
Conditions are an often-used pattern to include them in the status of CRs. A `Condition` represetns the latest available observations of an object's state. They are a convention as per the sig-architecture "api-conventions.yaml" https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

"They allow tools and **other controllers** to collect summary informations about resources without needing to understand resource-specific status details". For example the `kubectl wait` subcommand can block till a specific condition is met, e.g. `kubectl wait --for=condition=Ready pod/busybox1`

According to the api-convention Conditions are most useful when they follow some consistent conventions:
- The meaning of a Condition shouldn't be changed arbitrarity - it becomes part of the API (has same backwards- and forwards-compatibility concerns)
- controllers should apply their condition to a resource the first time they visit the resource, even if the status is Unknown => this lets lets users and components in system know that condition exists and controller is making progress on reconciling the resource!
- Should describe the current observed state, rather than curren tstate tranistions. Thus typically use adjective ("Ready", "OutOfDisk") or past-tense verb ("Succeeded", "Failed") rather than a present-tense ver ("Deploying")
  - The latter, intermediate states, may be indicated by setting the status of the condition to `Unknown`
  - For state Transitions that take a long time (e.g. more than 1 minute), it is reasonable to treat the trasition itself as an observed state!. In this case a Condition such as "Resizing" itself should not be transient and should instead be signalled using True/False/Unknown pattern.



## API Machinery: Phases vs Conditions
Discussion/Source: https://github.com/kubernetes/kubernetes/issues/7856
Issue by Briant Grant, lead architect behind Kubernetes API design

thockin: "orthogonal Conditions is really what we agreed on months ago"

### HOw to model the "Absence" of something?
thockin: "What's not clear [...] is how someone is supposred to know what th ABSENCE of a Condition means."

Given the example of an async external LB assignment, where we might suggest that a Service should remain "not-ready" until ENdpoints are "non-empty". So how to denote that Service.Status.Phase is "not-ready"? With a "Pending" Condition, that the EndpointsController has to clear?


### Conditions: What should {type: ready, status: "Unknown"} mean?
"it might or might not be ready, we don't know"
<hr> => meaning: "Unknown" is a fact about the *writer* of the conditinon, and not a claim about the *object*!
<hr> Also generally it is a hint that the object is still being reconciled


### API Machinery: What is the difference between a Phase and a Condition?
A phase follows state machine semantics 

The pattern of using phase is deprecated, meaning new extensions to k8s, like this operator, shouldn't use it. (Source api-conventions: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)


### K8s API: Why are Conditions more extensible (compared to phases)?
adding new conditions doesn't (and shouldn't) invalidate decisions based on existing conditions
=> They're orthogonal
<hr> better suited to reflect conditions/properties that may toggle back and forth and/or that may not be mutually exclusive
<hr> While phases would attempt to reflect a single state that encompasses a set of conditions - "rather than distributing logic that combines multiple conditions or orther properties for common inferences, such as whether the scheduler may schedule new pods to a node, we should expose those properties/conditions explicitly, for similar reasons as why we explicitly return fields containing default values"


## K8s API: Why are enumerated states not extensible?



# Deployment
The controller needs access to the Kubernetes-API, if that's the case it can be deployed. So this can be:
- Inside the cluster inside a pod
- outside the cluster, as just a linux process running in your dev environment shell!
- Or with OLM (operator lifecycle manager) though an operator catalog - really the holy grail for an operator-sdk developed operator

## Outside Cluster: locally
`make run` starts the go "manager" binary locally, subscribing to for watch events and whiping the cluster into the desired shape with Requests.

## In Cluster: via deployment
`make deploy` build and image and then deploy is as an k8s `Deployment`!

The k8s Deployment will be templated with kustomize and simply assume the manager binary is already available in the registry. The image its trying to deploy has the very generic name `controller:latest`... uhhh what? 
That's because the deploy target will template the deployment in config/default which has this value hardcoded.

So how do we build that image?
`make docker-build`

Then we can run it locally still:
`docker run --rm controller:latest`
But it will crash:
```logs
2025-06-05T22:11:40Z	ERROR	controller-runtime.client.config	unable to get kubeconfig	{"error": "invalid configuration: no configuration has been provided, try setting KUBERNETES_MASTER environment variable", "errorCauses": [{"error": "no configuration has been provided, try setting KUBERNETES_MASTER environment variable"}]}
sigs.k8s.io/controller-runtime/pkg/client/config.GetConfigOrDie
	/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.20.3/pkg/client/config/config.go:177
```
It crashes on creation of the Manager, while evaluating the aptly called `GetConfigOrDie()`

```go
mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
//.. step into GetConfigOrDie:
func GetConfigOrDie() *rest.Config {
	config, err := GetConfig()
	if err != nil {
		log.Error(err, "unable to get kubeconfig")  // <- there it is
		os.Exit(1)
	}
	return config
}
```
So it needs a kubeconfig, and it suggest we pr

I wonder if when we run the manager later in a pod, in the cluster, it will notice that it has a serviceAccount mounted and attempt to talk to the kubernetes api via the canonical `kubernetes.default.svc.cluster.local` kubernetes service.
  
Anyway lets first simple try to provide it via a volumemount:
`docker run --rm -v $HOME/.kube/config:/kubeconfig:ro -e KUBECONFIG=/kubeconfig controller:latest controller:latest`
caveat: ensure that the .kube/config has read rights for other (chmod a+r) or else the DOCKERFILE USER can't read it.
Anyway this gets the manager running but then it fails with a network issue issue:

```bash
	/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.20.3/pkg/internal/source/kind.go:64
2025-06-05T22:42:40Z	ERROR	controller-runtime.source.EventHandler	failed to get informer from cache	{"error": "failed to get server groups: Get \"https://myraspi:44949/api\": dial tcp: lookup myraspi on 192.168.0.1:53: no such host"}
```
Which is an issue with the container network not being able to lookup in my lan DNS. To add the lookup docker you can either:
- volume mount your /etc/hosts that has the `myraspi <ip>` entry or the more native way
- using the -- `--addh-host` option with the docker cli: `docker run --rm -v $HOME/.kube/config:/kubeconfig:ro -e KUBECONFIG=/kubeconfig --add-host myraspi:192.168.0.123 controller:latest controller:latest` 

# Testing
On each change to the controller I'd like to test a bunch of scenarios automatically, to ensure certain features still work and we're not regressing (thus so called "regression testing"). Some example tests are:
- basic sync feature: test synching configmap between two namespaces. Thus testing the core feature of this operator. First creating and then keeping in sync
- testing deletion, so cleanup scenarios
- eventually multitenancy: show that users, really service accounts, are allowed to sync only between namespace that they have access to!

## Operator-SDK Testing
Source: https://sdk.operatorframework.io/docs/building-operators/golang/testing/

Operator-SDK recomments `envtest` to write tests for Operators projects as it: 
- has a more active contributor commmunity, 
- more mature than Operator SDK's test framework
- offline: doesn't require an actual cluster to run tests (huge benefit in CI scnearios)

### Framework suppport
the file `internal/controller/suite_test.go` is created when a controller is scaffolded by the tool.
The file contians:
- boilerplate for executing integration tests using `envtests` with `ginkgo` and `gomega`

## `envtest`
Source: https://book.kubebuilder.io/reference/envtest.html
### What is `envtest`
A go package that provides libraries for integration testing for controllers by starting a local control plane
<hr> Control plane binaries (etcd and kube-apiserver, but "without kubelet, controller-manager or other components.") are loaded by default from `/usr/local/kubebuilder/bin` this can be overridden with the envvar `KUBEBUILDER_ASSETS` (this is set in the Makefile target `test:`!)


### Setup
Call `make test:` which will call the Makefile target `envtest` 
- the `envtest makefile target`: will download the `setup-envtest` cli and put it in `bin/`
Then the target `test` is called:
- the `test makefile target`: ensures that the Kubernetes API server binaries are downloaded to the `bin/k8s` folder including: `etcd`, `kubectl` and the `kube-apiserver`!

this is done via the go cli command in the Makefile: `bin/setup-envtest use <version-to-download> --bin-dir bin/ -p path`

The package `github.com/k-stz/config-weaver-operator/internal/controller` contians the envtest setup and will thus run envtest as part of the tests. The entry point for the tests is `internal/controller/suite_test.go`. Which contains `TestControllers` which invokes functions enrolling the whole Ginkgo testing process like `RunSpec(...)`.

Get the Ginkgo cli `go install github.com/onsi/ginkgo/v2/ginkgo`and run the test with it, for example with -v to get a verbose output including the DSL outlines wiht BeforeSuite, AfterSuite and the prose inbetween.

Run tests with `ginkgo -v`, this calls `go test` under the hood but has mor

### Gingko
`internal/controller/suite_test.go` the Before- and AfterSuite. The BeforeSuite sets up the envtest a Fake for for the Kubernetes cluster using `envtest` consisting of only etcd and kube-apiserer. So it should only be about the api-machinery path from the Client->kube-apiserver->etcd but not actually create or change actual Pods running on the cluster. 

- A "spec": refers a test in Ginkgo, in order to differentiate them from the traditional go `testing` package tests.
- Ginkgo suite: a collection of GInkgo specs in a given package

### Implemented Testcases
The following test cases are implemented to prove the basic contract that the ConfigMapSync controller should always fulfill.

- [x] BeforeSuite: Start an `envtest` kubernetes fake cluster
- [x] AfterSuite: tear the `envtest` cluster down after all ginkgo specs have ran

- [x] BeforeEach:
  - [x] Create a source ConfigMap, without error
  - [x] Then Create sample ConfigMapSync referencing the source ConfigMap, without Error

- [x] Testcases:
  - [x] Check if target namespace contains a synced target ConfigMap
  - [x] Validate that the source and target ConfigMaps Data-fields match 
  - [x] Change the source ConfigMap and validate that the target ConfigMap still is in sync with the sourceConfigMap

- [x] AfterEach:
  - [x] Cleanup the source ConfigMap
  - [x] Cleanup the sample ConfigMapSync referencing the source ConfigMap, without Error


# Multitenancy implementation 
This section documents the necessary steps and concepts needed to add multitenancy to the operator. Before we consider the steps, lets consider the initital starting position the operator was prior to adding any multitenancy features: 
THe primary resource `ConfigMapSync` was clusterscoped and it described for the operator a source ConfigMap it shall sync to a list of given target namespaces. There was no controlmechanism in place as it allows users to overwritte or copy each others ConfigMaps across the whole cluster. So how can we add multitenancy to this setup?

## Namespaced `ConfigMapSync`
- When switching form Cluster-scoped to namespace scoped, the `envtest`-testsuite loudly failed stating that the codemarker  must be `Namespaced` (not `Namespace` as was the typo) for the ConfigMapSync struct. Updated codemark: `//+kubebuilder:resource:scope=Namespaced,path=configmapsyncs,shortName=cms;cmsync`
- then it informed me that the testcode doesn't set the namespace of the ConfigMapSync resource

## Adjust Watches for cross-namespace secondary resources
Documentation: https://book.kubebuilder.io/reference/watching-resources

Inititally the `ConfigMapSync`  - the so-called  **"Primary Resource"** of our controller - was clusterscoped and the ConfigMaps that it synced were owned by it using an OwnerReference. The `OwnerReference` on the Object under `.metadata.ownerReference` is used for garbage collection (deleting the owner, also deletes the owned resource) and is set explicitly via controller logic. Secondly the owner reference can be comfortably used as the hook for watches, such that when the owned resource changes the primary resource controller gets a watch event triggering its reconciliation. As you might imagine that's a desired feature in this operator.

The problem is that owned resource can only trigger watch events when they are owned by a clusterscoped primary resource or in the same namespace, as a namespaced resource. But that is no longer the case and, furthermore, our primary resource sync across namespaces, thus it will never be collocated with all configmaps namespaces. We need a new watch mechanism. For this the kubebuilder framework documentation has a section describing how to watch resources "which are NOT Owned by the Controller" that we can use. 

