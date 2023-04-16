package main

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
)

func main() {
	// Parse command line arguments
	if len(os.Args) < 3 {
		fmt.Println("Usage: append-cert <image-url> <ca-certificates-file>")
		os.Exit(1)
	}
	imageURL := os.Args[1]
	caCertFile := os.Args[2]

	// Read the contents of the local CA certificates file
	caCertBytes, err := ioutil.ReadFile(caCertFile)
	if err != nil {
		fmt.Printf("Failed to read CA certificates file %s: %s\n", caCertFile, err)
		os.Exit(1)
	}

	// Fetch the remote image and its manifest
	ref, err := name.ParseReference(imageURL)
	if err != nil {
		fmt.Printf("Failed to parse image URL %s: %s\n", imageURL, err)
		os.Exit(1)
	}

	img, err := fetchImage(imageURL)
	if err != nil {
		fmt.Printf("Failed to fetch image %s: %s\n", imageURL, err)
		os.Exit(1)
	}
	newImg := newImage(img, caCertBytes, imageURL)

	newRef := ref.Context().Tag("withcerts")

	// Push the modified image back to the registry
	err = remote.Write(newRef, newImg, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		fmt.Printf("Failed to push modified image %s: %s\n", imageURL, err)
		os.Exit(1)
	}

	fmt.Printf("Successfully appended CA certificates to image %s\n", imageURL)
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

func newImage(old v1.Image, caCertBytes []byte, url string) v1.Image {
	imgCaCertBytes, err := extractCACerts(old)
	if err != nil {
		fmt.Printf("Failed to extract CA certificates from image %s: %s\n", url, err)
		os.Exit(1)
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
	newTar.Write(newCaCertBytes)
	newTar.Close()

	newImg, err := mutate.Append(old, mutate.Addendum{Layer: stream.NewLayer(io.NopCloser(&buf))})
	if err != nil {
		fmt.Printf("Failed to append modified CA certificates to image %s: %s\n", url, err)
		os.Exit(1)
	}
	return newImg
}
