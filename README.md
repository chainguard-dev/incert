# incert

`incert` is a Go program that appends CA certificates to Docker images and pushes the modified image to a specified registry.

(this used to be named `certko`)

## Installation

To install `incert`, use the following command:

```bash
$ go install github.com/chainguard-dev/incert@latest
```

## Flags

`incert` supports the following flags:

```shell
  -ca-certs-file string
        The path to the local CA certificates file
  -ca-certs-image-url string
        The URL of an image to extract the CA certificates from
  -dest-image-url string
        The URL of the image to push the modified image to
  -image-cert-path string
        The path to the certificate file in the image (optional) (default "/etc/ssl/certs/ca-certificates.crt")
  -image-url string
        The URL of the image to append the CA certificates to
  -output-certs-path string
        Output the (appended) certificates file from the image to a local file (optional)
  -replace-certs
        Replace the certificates in the certificate file instead of appending them
```

## Example

To append a corporate CA certificate to an image, use the following command:

```bash
$ incert -image-url=mycompany/myimage:latest -ca-certs-file=/path/to/cacerts.pem -dest-image-url=myregistry/myimage:latest
```

This will append the certificates in `/path/to/cacerts.pem` to the `mycompany/myimage:latest` image and push the modified image to `myregistry/myimage:latest`.

For security, `incert` outputs the pushed image reference (with digest) to stdout:

```bash
$ incert --image-url=gcr.io/dlorenc-chainguard/wolfi-base --ca-certs-file mycert.pem --dest-image-url gcr.io/dlorenc-chainguard/wolfi-base:new
Successfully appended CA certificates to image gcr.io/dlorenc-chainguard/wolfi-base:withcerts
gcr.io/dlorenc-chainguard/wolfi-base:withcerts@sha256:0cd4278e8072df5acd4956eb58ecba73024de47d9ceace3f0d39fb64e1b01ca6
```

## Authentication

incert uses standard Docker credential helpers for authentication.
To configure your credential helper, please follow the instructions in the [Docker documentation](https://docs.docker.com/engine/reference/commandline/login/#credential-helpers).

## Certificate Formats

Certificate files should be pem encoded and ready to append to a list of other pem certificates.
They should look something like this:

```
-----BEGIN CERTIFICATE-----
MIIFVjCCAz6gAwIBAgIUQ+NxE9izWRRdt86M/TX9b7wFjUUwDQYJKoZIhvcNAQEL
BQAwQzELMAkGA1UEBhMCQ04xHDAaBgNVBAoTE2lUcnVzQ2hpbmEgQ28uLEx0ZC4x
FjAUBgNVBAMTDXZUcnVzIFJvb3QgQ0EwHhcNMTgwNzMxMDcyNDA1WhcNNDMwNzMx
MDcyNDA1WjBDMQswCQYDVQQGEwJDTjEcMBoGA1UEChMTaVRydXNDaGluYSBDby4s
THRkLjEWMBQGA1UEAxMNdlRydXMgUm9vdCBDQTCCAiIwDQYJKoZIhvcNAQEBBQAD
ggIPADCCAgoCggIBAL1VfGHTuB0EYgWgrmy3cLRB6ksDXhA/kFocizuwZotsSKYc
IrrVQJLuM7IjWcmOvFjai57QGfIvWcaMY1q6n6MLsLOaXLoRuBLpDLvPbmyAhykU
AyyNJJrIZIO1aqwTLDPxn9wsYTwaP3BVm60AUn/PBLn+NvqcwBauYv6WTEN+VRS+
GrPSbcKvdmaVayqwlHeFXgQPYh1jdfdr58tbmnDsPmcF8P4HCIDPKNsFxhQnL4Z9
8Cfe/+Z+M0jnCx5Y0ScrUw5XSmXX+6KAYPxMvDVTAWqXcoKv8R1w6Jz1717CbMdH
flqUhSZNO7rrTOiwCcJlwp2dCZtOtZcFrPUGoPc2BX70kLJrxLT5ZOrpGgrIDajt
J8nU57O5q4IikCc9Kuh8kO+8T/3iCiSn3mUkpF3qwHYw03dQ+A0Em5Q2AXPKBlim
0zvc+gRGE1WKyURHuFE5Gi7oNOJ5y1lKCn+8pu8fA2dqWSslYpPZUxlmPCdiKYZN
pGvu/9ROutW04o5IWgAZCfEF2c6Rsffr6TlP9m8EQ5pV9T4FFL2/s1m02I4zhKOQ
UqqzApVg+QxMaPnu1RcN+HFXtSXkKe5lXa/R7jwXC1pDxaWG6iSe4gUH3DRCEpHW
OXSuTEGC2/KmSNGzm/MzqvOmwMVO9fSddmPmAsYiS8GVP1BkLFTltvA8Kc9XAgMB
AAGjQjBAMB0GA1UdDgQWBBRUYnBj8XWEQ1iO0RYgscasGrz2iTAPBgNVHRMBAf8E
BTADAQH/MA4GA1UdDwEB/wQEAwIBBjANBgkqhkiG9w0BAQsFAAOCAgEAKbqSSaet
8PFww+SX8J+pJdVrnjT+5hpk9jprUrIQeBqfTNqK2uwcN1LgQkv7bHbKJAs5EhWd
nxEt/Hlk3ODg9d3gV8mlsnZwUKT+twpw1aA08XXXTUm6EdGz2OyC/+sOxL9kLX1j
bhd47F18iMjrjld22VkE+rxSH0Ws8HqA7Oxvdq6R2xCOBNyS36D25q5J08FsEhvM
Kar5CKXiNxTKsbhm7xqC5PD48acWabfbqWE8n/Uxy+QARsIvdLGx14HuqCaVvIiv
TDUHKgLKeBRtRytAVunLKmChZwOgzoy8sHJnxDHO2zTlJQNgJXtxmOTAGytfdELS
S8VZCAeHvsXDf+eW2eHcKJfWjwXj9ZtOyh1QRwVTsMo554WgicEFOwE30z9J4nfr
I8iIZjs9OXYhRvHsXyO466JmdXTBQPfYaJqT4i2pLr0cox7IdMakLXogqzu4sEb9
b91fUlV1YvCXoHzXOP0l382gmxDPi7g4Xl7FtKYCNqEeXxzP4padKar9mK5S4fNB
UvupLnKWnyfjqnN9+BojZns7q2WwMgFLFT49ok8MKzWixtlnEjUwzXYuFrOZnk1P
Ti07NEPhmg4NpGaXutIcSkwsKouLgU9xGqndXHt7CMUADTdA43x7VF8vhV929ven
sBxXVsFy6K2ir40zSbofitzmdHxghm+Hl3s=
-----END CERTIFICATE-----
```