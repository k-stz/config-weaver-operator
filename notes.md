# Setup `k3d`
```sh
# create registry with specific port
k3d registry create myregistry -p 43761
# create cluster with dedicated registry
k3d cluster create mycluster --agents 1 -p "8080:80@loadbalancer" -p "8443:443@loadbalancer" --registry-use k3d-myregistry
```

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




## Deploy pod using image from internal registry
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
operator-sdk create api --group weaver --version v1alpha
1 --kind ConfigMapSync
```


The resulting APIGroup  will be what you pass as --group in kubebuilder `create api` plus what you set as --domain in `operator-sdk init --domain`.
So in this case will be `weaver.example.com`