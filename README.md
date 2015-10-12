# Etcd Service

This project provides an Etcd service that can run inside of Kubernetes.

There are two parts:

- An in-cluster discovery service (`deis-etcd-discovery`)
- A dynamic etcd cluster (`deis-etcd`)

Both parts run inside of any Kubernetes 1.0+ cluster. No modifications
need to be made to Kubernetes.

## Usage

Currently, this project is under heavy development. Eventually,
installing will be a matter of running a few `kubectl` commands, but
right now it needs a little more.

Assuming you have `kubectl`, `docker`, a Docker registry, and `go` 1.5.1
or greater, you can do this:

Get your dev environment ready:
```
$ go get github.com/Masterminds/glide
$ $GOPATH/bin/glide install
$ export DEV_REGISTRY=your.docker.registry.url:port
```

Start the services and mount the secrets volumes:
```
$ make kube-service
```

Build it all, push it to Docker, and then load it into Kubernetes:
```
$ make all
```

You can check on things using kubectl:
```
$ kubectl get pod
```

Note that if your registry is insecure, you need to configure
Kubernetes' Docker instances to allow the `--insecure-registry`.

## Notes

1. Right now, the discovery token is hard-coded into the secret. Feel
   free to change it to meet your needs.
2. Persistent storage for Etcd is not yet implemented, though it will be
   in short order.
3. The cluster is designed to be relatively self-healing. When a Pod
   dies, the pod that is spawned in its place will attempt to clean up
   mess.
4. The discovery instance of etcd is also used for an additional
   heartbeats layer, so it must stay running in order to help a failed
   cluster rebuild.
5. This project builds one Docker image that has two executable paths.
   If it is started with the `/bin/boot`, it will be an etcd cluster
   member. If it is started with `/bin/discovery` it will come up as a
   discovery service.

Finally, this distribution will _not_ work outside of Kubernetes unless
you do a lot of environment altering. It uses Kubernetes service
discovery environment variables, secrets volume mounts, and the
Kubernetes API server.
