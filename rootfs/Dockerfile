FROM alpine:3.1

RUN apk add --update-cache curl \
    && rm -rf /var/cache/apk/*

COPY rootfs /
WORKDIR /opt

# Install Etcd and EtcdCtl
RUN curl -sSL https://github.com/coreos/etcd/releases/download/v2.2.0-rc.0/etcd-v2.2.0-rc.0-linux-amd64.tar.gz -o /opt/etcd.tgz \
    && tar -zxf /opt/etcd.tgz \
    && mv /opt/etcd-v2.2.0-rc.0-linux-amd64/etcd /usr/local/bin/etcd \
    && mv /opt/etcd-v2.2.0-rc.0-linux-amd64/etcdctl /usr/local/bin/etcdctl \
    && chmod +x /usr/local/bin/etcd \
    && rm -rf /opt/etcd-v2.2.0-rc.0-linux-amd64 /opt/etcd.tgz

RUN chmod +x /bin/boot /bin/discovery

EXPOSE 2380 4100 2381
CMD ["/bin/boot"]
