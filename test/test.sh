#!/bin/bash

# Test script for incert
# Assumes incert is on path and docker is available

set -e
#set -x

cleanup() { 
  docker rm -f $NGINX
  rm selfsigned.pem
  rm selfsigned-key.pem
  rm selfsigned.csr
}

trap cleanup EXIT

# create self-signed cert
#cfssl selfsign www.example.net csr.json | cfssljson -bare selfsigned
docker run -v $PWD:/data --entrypoint /bin/sh -it -w /data cgr.dev/chainguard/cfssl:latest-dev -c "cfssl selfsign www.example.net csr.json | cfssljson -bare selfsigned"

# Run nginx with private key and cert
NGINX=$(docker run -p 8443:8443 -d \
  -v ./nginx.default.conf:/etc/nginx/conf.d/nginx.default.conf \
  -v ./selfsigned.pem:/etc/nginx/conf.d/cert.pem \
  -v ./selfsigned-key.pem:/etc/nginx/conf.d/key.pem \
  cgr.dev/chainguard/nginx)

# check insecure curl works
docker run --rm -it --network host --add-host example.com:127.0.0.1 cgr.dev/chainguard/curl:latest-dev -k https://example.com:8443

# now create container with cert and try
IMAGE=ttl.sh/incert/test$RANDOM:20m
incert --ca-certs-file selfsigned.pem --image-url cgr.dev/chainguard/curl:latest --dest-image-url $IMAGE
docker run --rm -it --network host --add-host example.com:127.0.0.1 $IMAGE https://example.com:8443
