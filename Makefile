# ! IMPORTANT ! We are no longer using `godeps` to run builds. You should
# 	be using the GO15VENDOREXPERIMENT flag, and your dependencies should
# 	all be in $DEIS/vendor
SHORT_NAME ?= etcd

# Set these if they are not present in the environment.
export GOARCH ?= amd64
export GOOS ?= linux
export MANIFESTS ?= ./manifests
export DEIS_REGISTRY ?= ${DEV_REGISTRY}/

# Non-optional environment variables
export GO15VENDOREXPERIMENT=1
export CGO_ENABLED=0

# Environmental details
BINDIR := rootfs/usr/local/bin
VERSION ?= 0.0.1-$(shell date "+%Y%m%d%H%M%S")
LDFLAGS := "-s -X main.version=${VERSION}"
IMAGE_PREFIX ?= deis
IMAGE := ${DEIS_REGISTRY}${IMAGE_PREFIX}/${SHORT_NAME}:${VERSION}
RC := ${MANIFESTS}/deis-${SHORT_NAME}-rc.json
DISCOVERY_RC := ${MANIFESTS}/deis-${SHORT_NAME}-discovery-rc.json

# Get non-vendor source code directories.
NV := $(shell glide nv)

# Set up the development environment
bootstrap:
	glide up

build:
	go build -o ${BINDIR}/boot -a -installsuffix cgo -ldflags ${LDFLAGS} boot.go
	go build -o ${BINDIR}/discovery -a -installsuffix cgo -ldflags ${LDFLAGS} discovery.go

info:
	@echo "Version:    ${VERSION}"
	@echo "Registry:   ${DEIS_REGISTRY}"
	@echo "Go flags:   GOOS=${GOOS} GOARCH=${GOARCH} CGO_ENABLED=${CGO_ENABLED}"
	@echo "Image:      ${IMAGE}"
	@echo "Units:      ${MANIFESTS}"

clean:
	-rm rootfs/bin/boot

docker-build: build
	docker build --rm -t ${IMAGE} rootfs

docker-push:
	docker push ${IMAGE}

kube-delete:
	-kubectl delete rc deis-etcd-1
	sleep 5

kube-delete-all: kube-delete
	-kubectl delete service deis-etcd-discovery
	-kubectl delete rc deis-etcd-discovery
	-kubectl delete service deis-etcd-1
	-kubectl delete secret deis-etcd-discovery-token

kube-rc:
	@# The real pattern to match is v[0-9]+.[0-9]+.[0-9]+-[0-9]+-[0-9a-z]{8}, but
	@# we want to find broken versions, too.
	perl -pi -e "s|[a-z0-9.:]+\/deis\/etcd:[0-9a-z-.]+|${IMAGE}|g" ${RC} ${DISCOVERY_RC}
	-kubectl create -f ${DISCOVERY_RC}
	@echo "Pause for discovery service to come online."
	sleep 15
	kubectl create -f ${RC}

kube-update:
	perl -pi -e "s|[a-z0-9.:]+\/deis\/etcd:[0-9a-z-.]+|${IMAGE}|g" ${RC} ${DISCOVERY_RC}
	kubectl update -f ${DISCOVERY_RC}
	kubectl update -f ${RC}

kube-service: kube-secrets
	-kubectl create -f ${MANIFESTS}/deis-etcd-discovery-service.json
	-kubectl create -f ${MANIFESTS}/deis-etcd-service.json

kube-secrets:
	-kubectl create -f ${MANIFESTS}/deis-etcd-discovery-token.json

test:
	@#go test ${NV} # No tests for startup scripts.
	@echo "Implement functional tests in _tests directory"

all: build docker-build docker-push kube-clean kube-rc test

.PHONY: build clean docker-build docker-push all kube-clean kube-rc kube-service info kube-secrets kube-delete-all
