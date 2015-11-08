FROM alpine:3.2

COPY . /

# Install Etcd and EtcdCtl
RUN apk add --update-cache curl tar \
    && curl -sSL https://github.com/coreos/etcd/releases/download/v2.2.1/etcd-v2.2.1-linux-amd64.tar.gz \
    | tar -vxz -C /usr/local/bin --strip=1 etcd-v2.2.1-linux-amd64/etcd etcd-v2.2.1-linux-amd64/etcdctl \
    && apk del --purge curl tar \
    && rm -rf /var/cache/apk/*

RUN chmod +x /bin/boot /bin/discovery

EXPOSE 2380 4100 2381
CMD ["/bin/boot"]
