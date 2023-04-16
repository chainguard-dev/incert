package main

import (
	"archive/tar"
	"bytes"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
)

var (
	imageURL      string
	caCertFile    string
	destImageURL  string
	imageCertPath string
	outputCerts   string
)

func init() {
	flag.StringVar(&imageURL, "image-url", "", "The URL of the image to append the CA certificates to")
	flag.StringVar(&caCertFile, "ca-certs-file", "", "The path to the local CA certificates file")
	flag.StringVar(&destImageURL, "dest-image-url", "", "The URL of the image to push the modified image to")

	flag.StringVar(&imageCertPath, "image-cert-path", "/etc/ssl/certs/ca-certificates.crt", "The path to the certificate file in the image (optional)")
	flag.StringVar(&outputCerts, "output-certs-path", "", "Output the (appended) certificates file from the image to a local file (optional)")
}

func usage() {
	flag.Usage()
	os.Exit(1)
}

func main() {
	flag.Parse()

	if imageURL == "" || destImageURL == "" || caCertFile == "" {
		usage()
	}

	// Read the contents of the local CA certificates file
	caCertBytes, err := os.ReadFile(caCertFile)
	if err != nil {
		log.Fatalf("Failed to read CA certificates file %s: %s\n", caCertFile, err)
	}

	// Sanity check to make sure the caCertBytes are actually a list of pem-encoded certificates
	block, _ := pem.Decode(caCertBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		log.Fatalf("Failed to find any certificates in %s", caCertFile)
	}

	img, err := fetchImage(imageURL)
	if err != nil {
		log.Fatalf("Failed to fetch image %s: %s\n", imageURL, err)
	}

	newImg, err := newImage(img, caCertBytes)
	if err != nil {
		log.Fatalf("Failed to create new image: %s\n", err)
	}

	if outputCerts != "" {
		if err := os.WriteFile(outputCerts, caCertBytes, 0644); err != nil {
			log.Fatalf("Failed to write certificates to file %s: %s.\n", outputCerts, err)
		}
	}

	newRef, err := name.ParseReference(destImageURL)
	if err != nil {
		log.Fatalf("Failed to parse destination image URL %s: %s\n", destImageURL, err)
	}

	// Push the modified image back to the registry
	err = remote.Write(newRef, newImg, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		log.Fatalf("Failed to push modified image %s: %s\n", newRef.String(), err)
	}

	fmt.Fprintf(os.Stderr, "Successfully appended CA certificates to image %s\n", newRef.String())
	h, err := newImg.Digest()
	if err != nil {
		log.Fatalf("Failed to get digest of image %s: %s\n", newRef.String(), err)
	}
	fmt.Printf("%s@sha256:%s\n", newRef.String(), h.Hex)
}

// Fetch the remote image
func fetchImage(imageURL string) (v1.Image, error) {
	ref, err := name.ParseReference(imageURL)
	if err != nil {
		return nil, err
	}
	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// Extract the ca-certificates file from the remote image
func extractCACerts(img v1.Image) ([]byte, error) {
	flattened := mutate.Extract(img)
	tr := tar.NewReader(flattened)
	defer flattened.Close()
	// Read the files in the tar reader
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if hdr.Name == imageCertPath || hdr.Name == strings.TrimPrefix(imageCertPath, "/") {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("failed to find %s in remote image", imageCertPath)
}

func newImage(old v1.Image, caCertBytes []byte) (v1.Image, error) {
	imgCaCertBytes, err := extractCACerts(old)
	if err != nil {
		log.Fatalf("Failed to extract CA certificates from image: %s\n", err)
	}
	newCaCertBytes := append(imgCaCertBytes, caCertBytes...)

	// Create a new tar file with the modified ca-certificates file
	buf := bytes.Buffer{}
	newTar := tar.NewWriter(&buf)
	newTar.WriteHeader(&tar.Header{
		Name: imageCertPath,
		Mode: 0644,
		Size: int64(len(newCaCertBytes)),
	})
	if _, err := newTar.Write(newCaCertBytes); err != nil {
		return nil, err
	}
	newTar.Close()

	newImg, err := mutate.Append(old, mutate.Addendum{Layer: stream.NewLayer(io.NopCloser(&buf))})
	if err != nil {
		return nil, fmt.Errorf("failed to append modified CA certificates to image: %s", err)
	}
	return newImg, nil
}
