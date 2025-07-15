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


# Git Vultures: Many Repo Clones is fake engagement
ince I made this repository public on GitHub, it’s been cloned a lot daily, often by unique sources. Every time I push a new commit, I see a spike in unique clones. This isn’t because my work-in-progress operator is wildly popular or people desperately need the latest changes. Instead, we’re likely dealing with "Git Vultures," as described in Matthew Zito’s blog post, "Git Vultures: The Bots That Are Stalking Your Git Repositories". (Source: https://medium.com/@exbotanical/git-vultures-the-bots-that-are-stalking-your-git-repositories-ec81e06dcd04).
	
Simply put, Git Vultures are bots that scan repositories, hoping to find leaked credentials, secrets, or tokens. They probably chose this repo reling on a basic heuristic: my code involves these concepts, which the bots figure by searching for suggestive keywords using something like this:	

```bash
$ git grep -E 'token|secret' $(git rev-list --all)  | wc -l
1011
```
The bots probably use such a regex pattern or some shared "naughty"-list of suspicious terms to flag potential leaks.

*raises finger in a moralistic manner*: Remember that any commit is stored in its entirety in .git and thus in your GitHub repo. Simply removing the leaked information is not enough. If credentials have been commited and pushed they are considered leaked and thus compromised and need to be renewed. Only if you have commited them locally they can be considered salvagable, in the simplest case purge your local repo and reclone.

# Kubebuilder notes
## r.Get(), r.Update() ...
The operations to interact with a cluster are invoked as methods on the Reconciler struct `func (r ConfigMapSyncReconciler) Update`. What is used exactly? Let's inspect `r.Get()` for example when getting a `ServiceAccount`:
```go
sa := &v1.ServiceAccount{}
	if err := r.Get(ctx, saObjectKey, sa); err != nil {
```

Why can `r.Get()` be used on the struct? Because it implements the `controller.Reader` interface:
```go
type Reader interface {
	// Get retrieves an obj for the given object key from the Kubernetes Cluster.
	// obj must be a struct pointer so that obj can be updated with the response
	// returned by the Server.
	Get(ctx context.Context, key ObjectKey, obj Object, opts ...GetOption) error

	// List retrieves list of objects for a given namespace and list options. On a
	// successful call, Items field in the list will be populated with the
	// result returned from the server.
	List(ctx context.Context, list ObjectList, opts ...ListOption) error
}
```
Where is the implementation of `Get` and `List`? This is also implicit. The `ConfigMapSyncReconciler`-Struct contains an
embedded interface:
```go
type ConfigMapSyncReconciler struct {
	client.Client // <- Here. This is interface embedding
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	RunState      RunState
}
```
Interface embedding defines all methods on the struct, thus allowing the struct to be used in place of the interface. But it still needs to provide an implementation for `Get()` else it would
panic on invokation! So where is it? In the kubebuilder manager initilization code, so in `cmd/main.go`:
```go
if err = (&controller.ConfigMapSyncReconciler{
		Client:   mgr.GetClient(),  // <- here
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("configmapsync-controller"),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ConfigMapSync")
		os.Exit(1)
	}
```
The documentation of `GetClient()` states: "returns a client configuration with the Config", furthermore it also that the client may not be a fully "direct" client" -- it may read from cache and somewhere else it states only writes are direct. Digging into this mechanism unconvers that kubernetes-go-clients may be direct or indirect regardding reading from the cache or read/writing directly from the kube-apiserver.

Continuing, the "Config" that `GetClient()` is provided when creating a manager via the first
parameter to the `NewManager()` constructore:
```go
mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{ ... }
```
It's the `ctrl.GetConfigOrDie()`, it return "a `*rest.Config` for talking to the Kubernetes apiserver". This will search for all the canoncial locations like the serviceaccount token when in cluster, ~/.kube/config dir and look if he cli flag `--kubeconfig` is provided. 



## Subresource /status
To directly change the .status subresource you need to use
```bash
kubectl patch cms configmapsync-sample --type merge -p '{"status": {"test": "teststring"}}' --subresource=status
configmapsync.weaver.example.com/configmapsync-sample patched
```
OR simply with the `--subresouce` switch it works interactively! Like this:
```bash
kubectl edit cms configmapsync-sample --subresource status
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

### Refactor status.conditions
Inspired by cluster-logging-operator I want to set the conditions in during reconciliation in the process memory and then via a defered call at the end of each reconciliation apply it:

When this works the advantage is that if that works:
- we can set the conditions where they occur in the code
- track the condition via the inmemory .status fields instead of the thread-unprotectable Reconciler struct (the .RunState field) as is currently implemented

```go
readyCond := internalobs.NewCondition(obsv1.ConditionTypeReady, obsv1.ConditionUnknown, obsv1.ReasonUnknownState, "")

defer func() {
		updateStatus(r.Client, r.Forwarder, readyCond)
}()
```

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

### Adjust Watches for cross-namespace managed resources
The kubebuilder book will is referenced in this section: https://book.kubebuilder.io/reference/watching-resources

Inititally the `ConfigMapSync`  - the so-called  **"Primary Resource"** of our controller - was clusterscoped and the ConfigMaps that it synced were owned by it using an `OwnerReference`. The `OwnerReference` on the Object under `.metadata.ownerReference` is used for garbage collection (deleting the owner, also deletes the owned resource) and is set explicitly via ConfigMapSync- controller logic. Secondly the OwnerReference can be comfortably used as the hook for watches, such that when the owned resource changes the primary resource controller gets a watch event triggering its reconciliation. 

As you might imagine, failure to notice changes to a managed ConfigMap leads to inconsistency and hinders self-healing. The ConfigMaps might get out of sync up until a primary resource finally triggers reconciliation of our controller. Thus we need reconciliation to trigger also on changes to any of our managed ConfigMaps.

The problem is that owned resource can only trigger watch events when they are owned by a clusterscoped primary resource or in the same namespace, as a namespaced resource. But that is no longer the case and, furthermore, our primary resource sync across namespaces, thus it will never be collocated with configmaps in other namespaces. 

> In summary, we need reconciliation to trigger for `ConfigMaps` that are **outside** the `ConfigMapSync`'s namespace! 

So we need a new Watch mechanism. For this the kubebuilder framework documentation has a section describing how to watch resources "which are NOT Owned by the Controller" that we can lean on. 

But first, why don't we simply trigger reconciliation manually periodically every X seconds? This question is addressed by the kubebuilder book starting with a best practice stance "Kubernetes controllers are fundamentally event-driven". But I think the more precise formulation is "event-tiggered and level-driven" which boils down to the reconciliation-Loop being triggered by a create/update/delete event, NOT its content, and instead the logic then polls the state of the object. That is: we query "at what level the object is" (=> thus level-driven!) instead on focusing on the event itself, we focus on the intent in the `.spec` and attempt to reconcile it.

What are the advantages of "not polling periodically" best-practice?
- more efficient/performant: the system takes action when necessary, instead of on a fixed interval
- more responsive, this also aligns with KUbernetes' event-triggered architecture

That being said, most ontrollers also implement periodic resync, to guard against missed edges (e.g. through network partitioning). These are rare but important for robustness, and I know many tails were this lead to "magical" self-healing behaviour of the overall state of the cluster.

So now to the solution, how can we configure the watch mechanism. (For reference: https://book.kubebuilder.io/reference/watching-resources/secondary-resources-not-owned ). 

We adjust the watch configuration on the ctrl.Manager, which was previousy defined to watch on owned resources. Now we can configure custom `Watches` and we provide an event handler `handler.EnqueueRequestsFromMapFunc` which will run for every Watch event on ConfigMaps and implement a mapping for which ConfigMapSync-Object Reconciliation shall be triggered. The mapping logic uses a common controller pattern utilizing two annotation on the ConfigMap:
- `configmapsync.io/owner-name`
- `configmapsync.io/owner-namespace`
both together uniquely identify the ConfigMapSync-Object that manages them. The annotation were added originally when the Reconciliation-Loop first ran and synced the ConfigMaps, were it previously just set the OwnerRefernce it now sets these annotations.
```go
// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&weaverv1alpha1.ConfigMapSync{}).
		Watches(&v1.ConfigMap{}, // watch ConfigMaps
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
				annotations := obj.GetAnnotations()
				name, nameOk := annotations["configmapsync.io/owner-name"]
				namespace, nsOk := annotations["configmapsync.io/owner-namespace"]
				if nameOk && nsOk {
					return []reconcile.Request{
						{
							// maps the watched event the reconciler of specified object!
							NamespacedName: types.NamespacedName{
								Name:      name,
								Namespace: namespace,
							},
						},
					}
				}
				// If the label is not present or doesn't match, don't trigger reconciliation!
				return []reconcile.Request{} // = don't trigger reconcile!
			}),
			// Predicate for efficiency
			builder.WithPredicates(updatePredConfigMap),
		).
		Complete(r)
```

Furthermore, we also provide a kubebuilder predicate on the watch with the third and last paramter to Watches `builder.WithPredicates(updatePredConfigMap)` here we pass a function that is defined below `updatePredConfigMap`, which allows us to _filter_ watch events **before** they are even passed to the `EnqueueRequestsFromMapFunc` request mapper.

In the predicate function we pass all delete and create events along, but for update-events we only pass through those that:
- have the annotations above (TODO not implemented yet) 
- and for which the `.data` key was changed.

This greatly reduces the mapping effort later and ensures reconiliation only runs when necessary.

```go
var updatePredConfigMap predicate.Funcs = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {

    if ! hasOwnerAnnotations((*v1.ConfigMap)) {
       return false
    }


		oldObj := e.ObjectOld.(*v1.ConfigMap)
		newObj := e.ObjectNew.(*v1.ConfigMap)
		// Trigger reconciliation only ConfigMap Data changes
		changed := maps.Equal(oldObj.Data, newObj.Data)
		if !changed {  // only runs when .data field was changed  update event
			return true
		}
		return false
	},
	// Allow create events
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	// Allow delete events
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
	// Allow generic events (e.g., external triggers)
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
}
```



### ArgoCD Operator
Does the resource mapping via annotation:
CR-mapping

```go
// source https://github.com/argoproj-labs/argocd-operator/blob/655113a17854883a9fe3f6a7315c14a1bc4bca35/controllers/argocd/custommapper.go#L18C1-L38C2
func (r *ReconcileArgoCD) clusterResourceMapper(ctx context.Context, o client.Object) []reconcile.Request {
	crbAnnotations := o.GetAnnotations()
	namespacedArgoCDObject := client.ObjectKey{}

	for k, v := range crbAnnotations {
		if k == common.AnnotationName {
			namespacedArgoCDObject.Name = v
		} else if k == common.AnnotationNamespace {
			namespacedArgoCDObject.Namespace = v
		}
	}

	var result = []reconcile.Request{}
	if namespacedArgoCDObject.Name != "" && namespacedArgoCDObject.Namespace != "" {
		result = []reconcile.Request{
			{NamespacedName: namespacedArgoCDObject}, // <- Requests takes NamespacedName directly!
		}
	}
	return result
}
```



## ConfigMapSync namespaced serviecaccount authority
For the next piece in multitenancy architecture we will constrain the authority of a namespaced `ConfigMapSync` (`CMS`) object to a given serviceaccount. The serviceaccount must be colocated with the `CMS` providing an intuitive RBAC logic, that if a user has access to a namespace and can create `CMS` in it, the user can also access any serviceaccount in the namespace and thus take actions on behalf of the authority granted to the servieaccount.

First lets allow our `ConfigMapSync` to refernce a servcieaccount

```go
type ConfigMapSyncSpec struct {
	// Name of ConfigMap and its namespace that should be synced
	Source Source `json:"source,omitempty"`
	// List of namespaces to sync Source Namespace to
	SyncToNamespaces []string `json:"syncToNamespaces,omitempty"`

	// ServiceAccount points to the namespaced ServiceAccount that will be used to sync ConfigMaps. This is to provide multitenancy, constraining the authority of the syncing to the RBAC access of the given namespaced ServiceAccount
	//
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Service Account"
	ServiceAccount ServiceAccount `json:"serviceAccount"`
}

type ServiceAccount struct {
	// Name of the ServiceAccount to use to sync ConfigMaps. The ServiceAccount is created by the administrator
	//
	// +kubebuilder:validation:Pattern:="^[a-z][a-z0-9-]{2,62}[a-z0-9]$"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ServiceAccount Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`
}

```

First we add the field to the `...Spec` struct and introduce a new struct type `ServiceAccount`. Why not simply use a string? Furthermore this might be considered bad for usability as now we need to set the value like this:
```yaml
spec:
  serviceAccount:
     name: default
# instead of simply
  serviceAccount: default
```
and also the user expects more fields and a single "name" field feels probably superfluous.


The advantages are as follows:
- Extensibility: We can later expand the ServieAccount struct with additional fields, for example autoGenerate=bool without breaking the API, which would be the case when switching form a string value to an object value (= struct) 
- Clearer semantics: the field "name" clearly implies the name of the ServiceAccount, not its token

The next thing I want to stress is that  kubebuilder provides validation via codemarkers to provide a regex pattern that the field must match. See the entry `// +kubebuilder:validation:Pattern:="^[a-z][a-z0-9-]{2,62}[a-z0-9]$"`  This will be baked into the CRD OpenAPI schema and will be enforced by the apimachinery! This ensures serviceaccount will ALWAYS be valid words, alleviating that validation burder from the controller logic. The validation will be done by the kube-apiserver, if it fails it will reject the change and the client issuing it will receive very clear feedback what failed and for what reason - neat!

## Defaulting the ServiceAccount
Simply add a codemarker above the field you want to default:
```go
type ConfigMapSyncSpec struct {
  // ...
  // +kubebuilder:default:={name: "default"}     #<- THIS ONE
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Service Account"
	ServiceAccount ServiceAccount `json:"serviceAccount,omitempty"`
```

Now the value will be defaulted whenever it is not supplied or deleted.

### ginkgo test regression
At this point the ginkgo test failed because the serviceAccount was empty. Even though it is defaulted. No really,  tested this twice, maybe the `envtest` kube-apiserver doesn't do defaulting. And secondly because it doesn't even find the "default" service Account, so had to create it as well.

Next it yielded a failling test with:
"insufficient permissions on service account system:serviceaccount:default:default. Not authorized to create, update or delete configmaps in the following namespaces [target-ns default],"

Which was just perfect so a rolebinding was needed allowing it. 

## Validate the ServiceAccount
- Check whether the serviceaccount exists in the namespace
- introduce .status condition for this! "ValidServiceAccount"

Lean on implementation for the ClusterLogForwarder:

```go
// Source: cluster-logging-operator/internal/validations/observability/validate.go


// ValidateServiceAccountPermissions validates a service account for permissions to collect
// inputs specified by the CLF.
// i.e. collect-application-logs, collect-audit-logs, collect-infrastructure-logs
func validateServiceAccountPermissions(k8sClient client.Client, inputs sets.String, hasReceiverInputs bool, serviceAccount corev1.ServiceAccount, clfNamespace, name string) error {
	if inputs.Len() == 0 && hasReceiverInputs {
		return nil
	}
	if inputs.Len() == 0 {
		err := errors.NewValidationError("There is an error in the input permission validation; no inputs were found to evaluate")
		log.Error(err, "Error while evaluating ClusterLogForwarder permissions", "namespace", clfNamespace, "name", name)
		return err
	}
	var err error
	var username = fmt.Sprintf("system:serviceaccount:%s:%s", serviceAccount.Namespace, serviceAccount.Name)

	// Perform subject access reviews for each spec'd input
	var failedInputs []string
	for _, input := range inputs.List() {
		log.V(3).Info("[ValidateServiceAccountPermissions]", "input", input, "username", username)
		sar := createSubjectAccessReview(username, allNamespaces, "collect", "logs", input, obs.GroupName)
		log.V(3).Info("SubjectAccessReview", "obj", utilsjson.MustMarshal(sar))
		if err = k8sClient.Create(context.TODO(), sar); err != nil {
			return err
		}
		// If input is spec'd but SA isn't authorized to collect it, fail validation
		log.V(3).Info("[ValidateServiceAccountPermissions]", "allowed", sar.Status.Allowed, "input", input)
		if !sar.Status.Allowed {
			failedInputs = append(failedInputs, input)
		}
	}

	if len(failedInputs) > 0 {
		return errors.NewValidationError("insufficient permissions on service account, not authorized to collect %q logs", failedInputs)
	}

	return nil
}
```

## TokenRequest the ServiceAccount

## Creating a `TokenRequest` in Kubernetes: Solving the Subresource Mystery

When working with Kubernetes, I ran into an issue while trying to create a `TokenRequest` in my controller code using Kubebuilder. It threw errors that left me scratching my head, especially since the same operation worked seamlessly with the `kubectl` CLI. Let’s dive into what happened, why it happened, and how to properly create a `TokenRequest` in a Kubebuilder controller.

### The kubectl Success Story
Using kubectl, creating a bound token for a service account like `system:serviceaccout:defaul:default` is straightforward. For example:
```bash
# create bound token for `system:serviceaccout:defaul:default`
$ kubectl create token default -n default
eyJhbGciOiJSUzI1NiIsImtpZCI6I... # token is printed
```

You can also inspect the resulting TokenRequest object in YAML format:
```bash
$ kubectl create token default -n default -o yaml
apiVersion: authentication.k8s.io/v1
kind: TokenRequest
metadata:
  creationTimestamp: "2025-07-12T14:04:32Z"
  name: default
  namespace: default
spec:
  audiences:
  - https://kubernetes.default.svc.cluster.local
  - k3s
  boundObjectRef: null
  expirationSeconds: 3600
status:
  expirationTimestamp: "2025-07-12T15:04:32Z"
  token: eyJhbGci...the-token-here
```

"Ok, this looks promising", I thought, "Let's craft an example YAML manifest from it":
```yaml
# file: tokenrequest.yaml"
apiVersion: authentication.k8s.io/v1
kind: TokenRequest
metadata:
  # creationTimestamp: "2025-07-12T14:02:49Z"
  name: default
  namespace: default
spec:
  audiences:
  - https://kubernetes.default.svc.cluster.local
  - k3s
  boundObjectRef: null
  expirationSeconds: 3600
```
I applied it with kubectl apply -f tokenrequest.yaml -o yaml, expecting the same result. Instead, I got this error:
```bash
$ kubectl create -f tokenrequest.yaml -o yaml
error: resource mapping not found for name: "default" namespace: "default" from "tokenrequest.yaml": no matches for kind "TokenRequest" in version "authentication.k8s.io/v1"
ensure CRDs are installed first
```

What gives? Surely the `TokenRequest` Resource exists, how else would the `kubectl` CLI make the request? 

## The Mystery: Why Doesn’t TokenRequest Behave Like a Normal Resource?
I initially suspected TokenRequest might be a special resource, like `TokenReview`, which is create-only and therefore not listable via `kubectl get`. However, checking the API resources with `kubectl api-resources` confirmed that TokenRequest isn’t even listed. This was a clue that something was different.


To dig deeper, I inspected the OpenAPI spec for my cluster using:
```bash
kubectl get --raw /openapi/v2 | jq .
```

Or via the Kubernetes API Reference

API Reference: https://kubernetes.io/docs/reference/kubernetes-api/authentication-resources/token-request-v1/
At first glance it appears to be a normal resource the typical basic schema fields: apiVersion, kind, metadata, spec and status. The only hint that something is of is its allowed Operations section, it only allows this POST request:


This command provides a tailored view of the resources available in your cluster. Alternatively, the Kubernetes API reference for TokenRequest offers more details. 

At first glance it appeared to be a typical Kubernetes with the usual suspects in as schema fields: apiVersion, kind, metadata, spec and status. The critical deatil that revealed something was off was in the “Operations” section:
```text
HTTP Request

POST /api/v1/namespaces/{namespace}/serviceaccounts/{name}/token
```

This endpoint indicates that TokenRequest isn’t a top-level resource like a Pod or ConfigMap. Instead, it’s a subresource of a ServiceAccount. Subresource provide special semantics for a parent resource. Like when you do magical stuff with on a Pod including `kubectl exec`, `kubectl log` or `kubectl port-forward`. Those are expressed as subresources on a Pod. 

For example, `kubectl port-forward pods/my-pod 80` interacts with the `portforward` subresource of a Pod via a POST request to `/api/v1/namespaces/{namespace}/pods/{name}/portforward`. Similarly a `kubectl create token` sends a POST request to the "token" subresource of a ServiceAccount. Well and its schema must fit a `TokenRequest`.

Key takeaway: 
> TokenRequest is a POST-only subresource of a ServiceAccount, not a standalone resource. This explains why my YAML manifest failed—Kubernetes doesn’t recognize TokenRequest as a top-level resource you can apply.



### Creating a TokenRequest in a Kubebuilder Controller
Now that we understand TokenRequest is a subresource, how do we create one programmatically in a Kubebuilder controller? In my ConfigMapSync controller, I was already updating a subresource—the .status field — using `r.Status().Update(ctx, cms)`. Could I use a similar approach for `TokenRequest`? Unfortunately, the `controller-runtime` client (used by Kubebuilder) doesn’t provide a direct method for interacting with the token subresource. Instead, we need to use the lower-level Kubernetes Go client (in the `client-go` package), which offers a `CreateToken()` method.

#### Step 1: Set up the Clientset
To use client-go, we need a clientset in our reconciler. The Kubebuilder manager already provides a configuration we can use to create one. First extend the reconciler struct to include the clientset:
```go
type ConfigMapSyncReconciler struct {
    client.Client
    Scheme    *runtime.Scheme
    Recorder  record.EventRecorder
    Config    *rest.Config
    Clientset *kubernetes.Clientset
}
```

Then, in `cmd/main.go`, initialize the clientset and pass it to the reconciler during setup:
```go
	clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		setupLog.Error(err, "unable creating clientset")
		os.Exit(1)
	}

	if err = (&controller.ConfigMapSyncReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorderFor("configmapsync-controller"),
		Config:    mgr.GetConfig(),
		Clientset: clientset,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ConfigMapSync")
		os.Exit(1)
	}
```

#### Step 2: Create the `TokenRequest`

With the clientset ready, we can use its CreateToken method to create a TokenRequest for a given ServiceAccount. Here’s an abbreviated example implementation:
```go
func (r *ConfigMapSyncReconciler) createTokenRequestFor(ctx context.Context, sa *v1.ServiceAccount) (*authenticationapi.TokenRequest, error) {
	// kubectl create token sa-name -n sa-namespace
	tokenRequest := &authenticationapi.TokenRequest{
		Spec: authenticationapi.TokenRequestSpec{...},
	}
	tokenRequest, err := r.Clientset.CoreV1().ServiceAccounts(sa.Namespace).CreateToken(ctx, sa.Name, tokenRequest, metav1.CreateOptions{})
	// ...
}
```

#### Why use `client-go`?
The client-go library provides a typed, explicit interface for interacting with Kubernetes resources and subresources, unlike the more generic controller-runtime client. In this case, CreateToken directly maps to the /api/v1/namespaces/{namespace}/serviceaccounts/{name}/token endpoint, making it the perfect tool for the job.

### Summary (TokenRequests creation): 
The TokenRequest mystery boils down to its nature as a subresource of ServiceAccount, not a standalone resource. This explains why kubectl apply failed and why kubectl create token works—it’s targeting the token subresource endpoint. By using the client-go library’s CreateToken method, we can create TokenRequest objects programmatically in a Kubebuilder controller.

This experience highlights the importance of understanding Kubernetes’ resource model, especially when working with subresources. If you’re building controllers, keep both controller-runtime and client-go in your toolkit—they complement each other for different use cases.

### Fix Ginkgo-test fail
TODO: Sadly after making TokenRequests work using the clientset go-client, the ginko-library now panics... I wonder if that's another limitation of it.

Solution:
- Because the reconciler-struct lacked the new Clientset field.
```go
controllerReconciler := &ConfigMapSyncReconciler{
				Client:    k8sClient,
				Scheme:    k8sClient.Scheme(),
				Clientset: clientset, // <- adding this fixed it
			}

```

## Perform action on behalf of the ServiceAccount
You can test the service account token via curl. You can't use `kubectl proxy` as the proxy will authenticate you. Instead extract the actual kube-apiserver address and make the request as follows.

```bash
export token="put here the service account token to test. In this example from default sa"
APISERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
# then make the request
# WORKS:
$ curl -H "Authorization: Bearer $token" -k $APISERVER/api/v1/namespaces/default/configmaps/sync-me-cm

# And that's how it looks like when it is forbidden:
$ curl -H "Authorization: Bearer $token" -k $APISERVER/api/v1/namespaces/kube-system/configmaps/sync-me-cm
{
  "kind": "Status",
  "apiVersion": "v1",
  "metadata": {},
  "status": "Failure",
  "message": "configmaps \"sync-me-cm\" is forbidden: User \"system:serviceaccount:default:default\" cannot get resource \"configmaps\" in API group \"\" in the namespace \"kube-system\"",
  "reason": "Forbidden",
  "details": {
    "name": "sync-me-cm",
    "kind": "configmaps"
  },
  "code": 403
}%     
```

