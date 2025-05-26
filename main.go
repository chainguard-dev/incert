/*
Copyright 2024 Chainguard, Inc.
SPDX-License-Identifier: Apache-2.0
*/

package main

import (
	"archive/tar"
	"bytes"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/stream"
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
	rootCmd.Flags().StringVar(&imageURL, "image-url", "", "The URL of the image to append the CA certificates to")
	rootCmd.Flags().StringVar(&caCertFile, "ca-certs-file", "", "The path to the local CA certificates file")
	rootCmd.Flags().StringVar(&caCertsImageURL, "ca-certs-image-url", "", "The URL of an image to extract the CA certificates from")
	rootCmd.Flags().StringVar(&destImageURL, "dest-image-url", "", "The URL of the image to push the modified image to")
	rootCmd.Flags().StringVar(&platformStr, "platform", "linux/amd64", "The platform to build the image for")

	rootCmd.Flags().StringVar(&imageCertPath, "image-cert-path", "/etc/ssl/certs/ca-certificates.crt", "The path to the certificate file in the image (optional)")
	rootCmd.Flags().IntVar(&ownerUserID, "owner-user-id", 0, "The user ID of the owner of the certificate file in the image (optional)")
	rootCmd.Flags().IntVar(&ownerGroupID, "owner-group-id", 0, "The group ID of the owner of the certificate file in the image (optional)")
	rootCmd.Flags().StringVar(&outputCerts, "output-certs-path", "", "Output the (appended) certificates file from the image to a local file (optional)")
	rootCmd.Flags().BoolVar(&replaceCerts, "replace-certs", false, "Replace the certificates in the certificate file instead of appending them")

	_ = rootCmd.MarkFlagRequired("image-url")
	_ = rootCmd.MarkFlagRequired("dest-image-url")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:          "incert",
	Short:        "Appends CA certificates to Docker images and pushes the modified image to a specified registry.",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return do(cmd, args)
	},
}

// Fetch the remote image
func fetchImage(imageURL string, platform v1.Platform) (v1.Image, error) {
	ref, err := name.ParseReference(imageURL)
	if err != nil {
		return nil, err
	}
	img, err := remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithPlatform((platform)))
	if err != nil {
		return nil, err
	}
	return img, nil
}

func do(_ *cobra.Command, _ []string) error {
	var platform v1.Platform

	if caCertFile == "" && caCertsImageURL == "" {
		return errors.New("either --ca-certs-file or --ca-certs-image-url must be provided")
	}

	if platformStr != "" {
		p, err := v1.ParsePlatform(platformStr)
		if err != nil {
			return fmt.Errorf("Failed to parse platform: %s", err)
		}
		platform = *p
	}

	// Get the cert bytes
	caCertBytes, err := getCertBytes(platform)
	if err != nil {
		return fmt.Errorf("Failed to get certificate bytes: %s", err)
	}

	// Sanity check to make sure the caCertBytes are actually a list of pem-encoded certificates
	block, _ := pem.Decode(caCertBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return fmt.Errorf("Failed to find any certificates in %s", caCertFile)
	}

	img, err := fetchImage(imageURL, platform)
	if err != nil {
		return fmt.Errorf("Failed to fetch image %s: %s\n", imageURL, err)
	}

	newImg, err := newImage(img, caCertBytes)
	if err != nil {
		return fmt.Errorf("Failed to create new image: %s\n", err)
	}

	if outputCerts != "" {
		if err := os.WriteFile(outputCerts, caCertBytes, 0644); err != nil {
			return fmt.Errorf("Failed to write certificates to file %s: %s.\n", outputCerts, err)
		}
	}

	newRef, err := name.ParseReference(destImageURL)
	if err != nil {
		return fmt.Errorf("Failed to parse destination image URL %s: %s\n", destImageURL, err)
	}

	// Push the modified image back to the registry
	err = remote.Write(newRef, newImg, remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("Failed to push modified image %s: %s\n", newRef.String(), err)
	}

	fmt.Fprintf(os.Stderr, "Successfully appended CA certificates to image %s\n", newRef.String())
	h, err := newImg.Digest()
	if err != nil {
		return fmt.Errorf("Failed to get digest of image %s: %s\n", newRef.String(), err)
	}

	fmt.Printf("%s@sha256:%s\n", newRef.String(), h.Hex)

	return nil
}

func getCertBytes(platform v1.Platform) ([]byte, error) {
	// Read the certs either from a local file or a remote image
	if caCertFile != "" {
		// Read the contents of the local CA certificates file
		caCertBytes, err := os.ReadFile(caCertFile)
		if err != nil {
			return []byte{}, fmt.Errorf("Failed to read CA certificates file %s: %s\n", caCertFile, err)
		}

		// Sanity check to make sure the caCertBytes are actually a list of pem-encoded certificates
		block, _ := pem.Decode(caCertBytes)
		if block == nil || block.Type != "CERTIFICATE" {
			return []byte{}, fmt.Errorf("Failed to find any certificates in %s", caCertFile)
		}
		return caCertBytes, nil
	} else {
		// Fetch the remote image and its manifest
		img, err := fetchImage(caCertsImageURL, platform)
		if err != nil {
			return []byte{}, fmt.Errorf("Failed to fetch image %s: %s\n", caCertsImageURL, err)
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

func newImage(old v1.Image, caCertBytes []byte) (v1.Image, error) {
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
	_ = newTar.WriteHeader(&tar.Header{
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
