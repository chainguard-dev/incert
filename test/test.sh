#!/bin/bash

# Test script for incert
# Assumes incert is at "../incert" and docker is available

set -e
set -x

cleanup() { 
  docker rm -f $NGINX
  rm selfsigned.pem
  rm selfsigned-key.pem
  rm selfsigned.csr
}

trap cleanup EXIT

# create self-signed cert
#cfssl selfsign www.example.net csr.json | cfssljson -bare selfsigned
docker run -v $PWD:/data --entrypoint /bin/sh -it -w /data cfssl/cfssl -c "cfssl selfsign www.example.net csr.json | cfssljson -bare selfsigned"

# Run nginx with private key and cert
NGINX=$(docker run -p 8443:8443 -d \
  -v ./nginx.default.conf:/etc/nginx/conf.d/nginx.default.conf \
  -v ./selfsigned.pem:/etc/nginx/conf.d/cert.pem \
  -v ./selfsigned-key.pem:/etc/nginx/conf.d/key.pem \
  cgr.dev/chainguard/nginx)

# check insecure curl works
docker run --rm -it --network host --add-host example.com:127.0.0.1 cgr.dev/chainguard/curl:latest-dev -k https://example.com:8443

# now create container with cert and try; this should produce an image index
IMAGE=ttl.sh/incert/test-default-$RANDOM:20m
../incert --ca-certs-file selfsigned.pem --image-url cgr.dev/chainguard/curl:latest --dest-image-url $IMAGE
docker run --rm -it --network host --add-host example.com:127.0.0.1 $IMAGE https://example.com:8443
docker manifest inspect $IMAGE | docker run --rm -i --entrypoint jq cgr.dev/chainguard/curl:latest-dev -e '.mediaType == "application/vnd.oci.image.index.v1+json"'

# test using platform argument; this should produce an image manifest
IMAGE=ttl.sh/incert/test-arm64-default-$RANDOM:20m
../incert --ca-certs-file selfsigned.pem --image-url cgr.dev/chainguard/curl:latest --platform linux/arm64 --dest-image-url $IMAGE
docker run --rm -it --network host --add-host example.com:127.0.0.1 $IMAGE https://example.com:8443
docker manifest inspect $IMAGE | docker run --rm -i --entrypoint jq cgr.dev/chainguard/curl:latest-dev -e '.mediaType == "application/vnd.oci.image.manifest.v1+json"'

# test against a specific manifest, rather than an index
IMAGE=ttl.sh/incert/test-image-$RANDOM:20m
CURL_DIGEST=$(docker manifest inspect cgr.dev/chainguard/curl:latest | docker run --rm -i --entrypoint jq cgr.dev/chainguard/curl:latest-dev -r '.manifests[] | select(.platform.architecture == "arm64" and .platform.os == "linux") | .digest')
../incert --ca-certs-file selfsigned.pem --image-url cgr.dev/chainguard/curl@$CURL_DIGEST --dest-image-url $IMAGE
docker run --rm -it --network host --add-host example.com:127.0.0.1 $IMAGE https://example.com:8443

# for manifests, the --platform flag should make no difference
IMAGE=ttl.sh/incert/test-image-amd64-$RANDOM:20m
../incert --ca-certs-file selfsigned.pem --image-url cgr.dev/chainguard/curl@$CURL_DIGEST --platform=linux/amd64 --dest-image-url $IMAGE
docker run --rm -it --network host --add-host example.com:127.0.0.1 $IMAGE https://example.com:8443
