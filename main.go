/*
Copyright 2024 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

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
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

var (
	imageURL        string
	caCertFile      string
	caCertsImageURL string
	destImageURL    string
	platformStr     string
	imageCertPath   string
	outputCerts     string
	replaceCerts    bool
	ownerUserID     int
	ownerGroupID    int
)

func init() {
	flag.StringVar(&imageURL, "image-url", "", "The URL of the image to append the CA certificates to")
	flag.StringVar(&caCertFile, "ca-certs-file", "", "The path to the local CA certificates file")
	flag.StringVar(&caCertsImageURL, "ca-certs-image-url", "", "The URL of an image to extract the CA certificates from")
	flag.StringVar(&destImageURL, "dest-image-url", "", "The URL of the image to push the modified image to")
	flag.StringVar(&platformStr, "platform", "", "The platform to build the image for")

	flag.StringVar(&imageCertPath, "image-cert-path", "/etc/ssl/certs/ca-certificates.crt", "The path to the certificate file in the image (optional)")
	flag.IntVar(&ownerUserID, "owner-user-id", 0, "The user ID of the owner of the certificate file in the image (optional)")
	flag.IntVar(&ownerGroupID, "owner-group-id", 0, "The group ID of the owner of the certificate file in the image (optional)")
	flag.StringVar(&outputCerts, "output-certs-path", "", "Output the (appended) certificates file from the image to a local file (optional)")
	flag.BoolVar(&replaceCerts, "replace-certs", false, "Replace the certificates in the certificate file instead of appending them")
}

func usage() {
	flag.Usage()
	os.Exit(1)
}

func main() {
	var platform *v1.Platform
	flag.Parse()

	if imageURL == "" || destImageURL == "" || (caCertFile == "" && caCertsImageURL == "") {
		usage()
	}

	if platformStr != "" {
		p, err := v1.ParsePlatform(platformStr)
		if err != nil {
			log.Fatalf("Failed to parse platform: %s", err)
		}
		platform = p
	}

	// Get the cert bytes
	caCertBytes, err := getCertBytes(platform)
	if err != nil {
		log.Fatalf("Failed to get certificate bytes: %s", err)
	}

	// Sanity check to make sure the caCertBytes are actually a list of pem-encoded certificates
	block, _ := pem.Decode(caCertBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		log.Fatalf("Failed to find any certificates in %s", caCertFile)
	}

	img, err := fetchImage(imageURL, platform)
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
	err = remote.Push(newRef, newImg, remoteOpts(platform)...)
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

// Image may be a v1.Image or a v1.ImageIndex. It allows us to operate
// generically across either.
type Image interface {
	// Digest returns the sha256 of this image's manifest.
	Digest() (v1.Hash, error)

	remote.Taggable
}

func remoteOpts(platform *v1.Platform) []remote.Option {
	rOpts := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
	if platform != nil {
		rOpts = append(rOpts, remote.WithPlatform(*platform))
	}

	return rOpts
}

// Fetch the remote image
func fetchImage(imageURL string, platform *v1.Platform) (Image, error) {
	ref, err := name.ParseReference(imageURL)
	if err != nil {
		return nil, err
	}
	if platform != nil {
		return remote.Image(ref, remoteOpts(platform)...)
	}

	desc, err := remote.Get(ref, remoteOpts(platform)...)
	if err != nil {
		return nil, err
	}
	switch desc.MediaType {
	case types.OCIImageIndex, types.DockerManifestList:
		return desc.ImageIndex()
	case types.OCIManifestSchema1, types.DockerManifestSchema2:
		return desc.Image()

	default:
		return nil, fmt.Errorf("unknown mime type: %v", desc.MediaType)
	}
}

func getCertBytes(platform *v1.Platform) ([]byte, error) {
	// Read the certs either from a local file or a remote image
	if caCertFile != "" {
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
		return caCertBytes, nil
	} else {
		// Fetch the remote image and its manifest
		ref, err := name.ParseReference(caCertsImageURL)
		if err != nil {
			log.Fatalf("Failed to parse image url %s: %s\n", caCertsImageURL, err)
		}
		img, err := remote.Image(ref, remoteOpts(platform)...)
		if err != nil {
			log.Fatalf("Failed to fetch image %s: %s\n", caCertsImageURL, err)
		}
		return extractCACerts(img)
	}
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

func newImage(old Image, caCertBytes []byte) (Image, error) {
	switch img := old.(type) {
	case v1.Image:
		return mutateImage(img, caCertBytes)
	case v1.ImageIndex:
		return mutateImageIndex(img, caCertBytes)
	default:
		return nil, fmt.Errorf("unexpected type: %t", img)
	}
}

func mutateImage(old v1.Image, caCertBytes []byte) (v1.Image, error) {
	var newCaCertBytes []byte
	if replaceCerts {
		newCaCertBytes = caCertBytes
	} else {
		imgCaCertBytes, err := extractCACerts(old)
		if err != nil {
			log.Fatalf("Failed to extract CA certificates from image: %s\n", err)
		}
		newCaCertBytes = append(append(imgCaCertBytes, caCertBytes...), '\n')
	}

	// Create a new tar file with the modified ca-certificates file
	buf := bytes.Buffer{}
	newTar := tar.NewWriter(&buf)
	newTar.WriteHeader(&tar.Header{
		Name: imageCertPath,
		Mode: 0644,
		Size: int64(len(newCaCertBytes)),
		Uid:  ownerUserID,
		Gid:  ownerGroupID,
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

func mutateImageIndex(old v1.ImageIndex, caCertBytes []byte) (v1.ImageIndex, error) {
	om, err := old.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	adds := []mutate.IndexAddendum{}
	for _, desc := range om.Manifests {
		var add mutate.Appendable

		switch desc.MediaType {
		case types.OCIImageIndex, types.DockerManifestList:
			idx, err := old.ImageIndex(desc.Digest)
			if err != nil {
				return nil, err
			}
			newIdx, err := mutateImageIndex(idx, caCertBytes)
			if err != nil {
				return nil, err
			}
			add = newIdx
		case types.OCIManifestSchema1, types.DockerManifestSchema2:
			img, err := old.Image(desc.Digest)
			if err != nil {
				return nil, err
			}
			newImage, err := mutateImage(img, caCertBytes)
			if err != nil {
				return nil, err
			}
			add = newImage
		default:
			return nil, fmt.Errorf("unknown mime type: %v", desc.MediaType)
		}

		adds = append(adds, mutate.IndexAddendum{
			Add: add,
			Descriptor: v1.Descriptor{
				URLs:        desc.URLs,
				MediaType:   desc.MediaType,
				Annotations: desc.Annotations,
				Platform:    desc.Platform,
			},
		})
	}

	nm := mutate.IndexMediaType(empty.Index, om.MediaType)
	nm = mutate.Annotations(nm, om.Annotations).(v1.ImageIndex)

	return mutate.AppendManifests(nm, adds...), nil
}
