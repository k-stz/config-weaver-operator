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

# Orphane cascading deletion
Astonishingly orphan deletion kubectl syntax is very picks:
```sh
# works:
kubectl delete configmapsyncs.weaver.example.com configmapsync-sample --cascade=orphan
# doesn't work!
# In this case the --cascade=orphan must use an equal sign for the option!
kubectl delete configmapsyncs.weaver.example.com configmapsync-sample --cascade orphan
```

# Switching CRD from namespaced to clusterscoped
For an easier initial implementaiton ConfigMapSync will be switched from namespaced to cluster scope. This is achieved by simple setting the codemarker `+kubebuilder:resource:scope=Cluster` above the API type definition and run `make manifests`. To combine it with shortnaming, you need to write it in one:
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
- A custom resource (e.g. ConfigMapSync) that lets a user define "sync this ConfigMap from nsA to nsB, nsC, etc". The operator (controller) to do the actual syncing.
- Multitenancy: users should only be able to sync between namespaces they have access to (e.g. RBAC allows them to create/update ConfigMaps in those namespaces).
- The operator runs with elevated permissions (as most do) but should act only on behalf of the requesting user, within their access scope.

The Challange:
Kubernetes controllers run as cluster components with service accounts that usually have broad access. So by default, your operator can sync ConfigMaps regardless of who created the ConfigMapSync object. That’s the problem. 

Approach:
Query the users permission on creation of object and embed it declaratively in the ConfigMapSync field? Then from then only those namespaces are syncable...? 

### Solution Proposal
Inspired by the openshift logging operator which for the ressource `ClusterLogForwarder` demands that you supply a serviceaccount via which rights logs are scraped/forwarded we can do the same.

The idea is to:
1. in the ConfigMapSync CR will be namespaced
2. It includes a ServiceAccount field where the user may only reference an SA in the same namespace
3. The operators:
 - reads the CR
 - uses the provided SA to impersonate or use its token
 - uses that identity to perform the actual syncing (e.g., create/update ConfigMaps in target namespace)
 4. Thus the ability to sync is bound by the RBAC rules attached to that SA, not the operator's own prvileges
 
### Evaluation of Solution

### Pros
- **multitenancy safe**: the operator is just a "conduit" and enforces actions only within the boudns of what the user's `ServiceAccount` is allowed to do!
- **RBAC-native**: Admins already know how to mange RBAC roles - we're reusing a familiar permission model
- **Namespace-isolated**: users can't smuggle in another namespace'S SA (will disallow referencing one from another namespace)
- **Scalable**: don't need to issue SARs for every action (less API traffic and conde complexity)

### Caveates / Refinements**:
Token Mounting:
- Token Mounting and Access: After accessing the token for that SA operators can create a token for the SA using  `TokenRequest API`
- Operator RBAC update: this requires the fllowing permissions
``` yaml
apiGroups: ["authentication.k8s.io"]
resources: ["serviceaccounts/token"]
verbs: ["create"]
```
- token should be short-lived and scoped to a minimal audience

Impersonation vs Token Use Tradeoffs
- 🔄 Impersonation:
  - You impersonate the user or SA by adding headers (Impersonate-User, Impersonate-Group)	Requires impersonate RBAC permission 
  - powerful and risky if misconfigured
- 🔑 TokenRequest (recommended): 
  - You get an actual token and create a new REST client with it	
  - Cleaner and easier to audit

=> TODO: review the TokenRequest solution

### Complication: ownerReference for namespaced Ressource 
To easy a multitenant implementation, namespaced CR (`ConfigMapSync`) is preferred. But the created ConfigMaps can't then set the ownerReference, as this is forbidden to point to a namespaced owner.
What do we need the ownerReference for:
- easy GC: when deleting CR all childs dependents will also be deletd
- easy dependent watch: the controller-runtime framework has a convenient watch implementation for owned objects. Now that it's missing, we'd need to implement a mechanism to watch changes on all the ConfigMaps and figure out the namespaced `ConifgMapSync`` that owns them.

Possible Solutions:
- Index + Watch with "Predicate Filters": 
  - The configmaps will be labeld with a owner-namespace and owner-name.
  - The ConfigMapSync CRs can be indexed! See "mgr.GetFieldIndexer"...
  - watch ConfigMaps with a custom event handler, instead of .Owns() use .Watches()... effectively triggereing the reconciliation handler for hte ConfigMapSync controller whenever a ConfigMap is updated with the labels set!
  - finalizers: to get GC back in, we can implement logic on the /finalizer subresource!




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


# Testing
On each change to the controller I'd like to test a bunch of scenarios automatically, to ensure certain features still work and we're not regressing (thus so called "regression testing"). Some example tests are:
- TODO name some

## Operator-SDK Testing
Source: https://sdk.operatorframework.io/docs/building-operators/golang/testing/

Operator-SDK recomments `envtest` to write tests for Operators projects as it: 
- has a more active contributor commmunity, 
- more mautre than Operator SDK's test framework
- offline: doesn't require an actual cluster to run tests (huge benefit in CI scnearios)

### Framework suppport
the file `controllers/suite_test.go` is created when a controller is scaffolded by the tool.
The file contians:
- boilerplate for executing integration tests using `envtests` with `ginkgo` and `gomega`

## `envtest`
### What is `envtest`
A go package that provides librareis for integration testing by starting a local control plane
<hr> Control plane binaries (etcd and kube-apiserver, but "without kubelet, controller-manager or other components.") are loaded by default from `/usr/local/kubebuilder/bin` this can be overridden with the envvar `KUBEBUILDER_ASSETS` (this is set in the Makefile target `test:`!)


