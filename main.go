package main

import (
	"archive/tar"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
)

var (
	imageURL     string
	caCertFile   string
	destImageURL string
)

func init() {
	flag.StringVar(&imageURL, "image-url", "", "The URL of the image to append the CA certificates to")
	flag.StringVar(&caCertFile, "ca-certs-file", "", "The path to the local CA certificates file")
	flag.StringVar(&destImageURL, "dest-image-url", "", "The URL of the image to push the modified image to")
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: certko --image-url <image-url> --ca-certificates-file <ca-certificates-file> --dest-image-url <dest-image-url>")
	os.Exit(1)
}

func main() {
	flag.Parse()

	if imageURL == "" || destImageURL == "" || caCertFile == "" {
		usage()
	}

	// Read the contents of the local CA certificates file
	caCertBytes, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		log.Fatalf("Failed to read CA certificates file %s: %s\n", caCertFile, err)
	}

	// Fetch the remote image and its manifest
	ref, err := name.ParseReference(imageURL)
	if err != nil {
		log.Fatalf("Failed to parse image URL %s: %s\n", imageURL, err)
	}

	img, err := fetchImage(imageURL)
	if err != nil {
		log.Fatalf("Failed to fetch image %s: %s\n", imageURL, err)
	}
	newImg, err := newImage(img, caCertBytes)
	if err != nil {
		log.Fatalf("Failed to create new image: %s\n", err)
	}

	newRef := ref.Context().Tag("withcerts")

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
		if hdr.Name == "/etc/ssl/certs/ca-certificates.crt" || hdr.Name == "etc/ssl/certs/ca-certificates.crt" {
			return ioutil.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("Failed to find /etc/ssl/certs/ca-certificates.crt in remote image")
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
		Name: "/etc/ssl/certs/ca-certificates.crt",
		Mode: 0644,
		Size: int64(len(newCaCertBytes)),
	})
	if _, err := newTar.Write(newCaCertBytes); err != nil {
		return nil, err
	}
	newTar.Close()

	newImg, err := mutate.Append(old, mutate.Addendum{Layer: stream.NewLayer(io.NopCloser(&buf))})
	if err != nil {
		return nil, fmt.Errorf("Failed to append modified CA certificates to image: %s\n", err)
	}
	return newImg, nil
}
