package cosign

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"context"
	"strings"
	"encoding/json"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/pkg/content"
	"github.com/rancherfederal/hauler/pkg/store"
	"github.com/rancherfederal/hauler/pkg/log"
	"github.com/rancherfederal/hauler/internal/mapper"
	"github.com/rancherfederal/hauler/pkg/reference"
	"github.com/rancherfederal/hauler/pkg/artifacts/file"
	"github.com/rancherfederal/hauler/pkg/artifacts/file/getter"
	"github.com/rancherfederal/hauler/pkg/apis/hauler.cattle.io/v1alpha1"
)

// VerifyFileSignature verifies the digital signature of a file using Sigstore/Cosign.
func VerifySignature(ctx context.Context, s *store.Layout, keyPath string, ref string) error {

	// Ensure that the cosign binary is installed or download it if needed
	cosignBinaryPath, err := ensureCosignBinary(ctx, s)
	if err != nil {
		return err
	}

	// Command to verify the signature using Cosign.
	cmd := exec.Command(cosignBinaryPath, "verify", "--insecure-ignore-tlog", "--key", keyPath, ref)

	// Run the command and capture its output.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error verifying signature: %v, output: %s", err, output)
	}

	return nil
}

// SaveImage saves image and any signatures/attestations to the store.
func SaveImage(ctx context.Context, s *store.Layout, ref string) error {

	// Ensure that the cosign binary is installed or download it if needed
	cosignBinaryPath, err := ensureCosignBinary(ctx, s)
	if err != nil {
		return err
	}

	// Command to verify the signature using Cosign.
	cmd := exec.Command(cosignBinaryPath, "save", ref, "--dir", s.Root)

	// Run the command and capture its output.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error adding image to store: %v, output: %s", err, output)
	}

	return nil
}

// LoadImage loads store to a remote registry.
func LoadImage(ctx context.Context, s *store.Layout, registry string, ropts content.RegistryOptions) error {

	//Ensure that the cosign binary is installed or download it if needed
	cosignBinaryPath, err := ensureCosignBinary(ctx, s)
	if err != nil {
		return err
	}

	// Command to verify the signature using Cosign.
	cmd := exec.Command(cosignBinaryPath, "load", "--registry", registry, "--dir", s.Root)

	// Conditionally add extra registry flags.
	if ropts.Insecure {
		cmd.Args = append(cmd.Args, "--allow-insecure-registry=true")
	}
	if ropts.PlainHTTP {
		cmd.Args = append(cmd.Args, "--allow-http-registry=true")
	}

	// Run the command and capture its output.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error adding image to store: %v, output: %s", err, output)
	}

	return nil
}

// ensureCosignBinary checks if the cosign binary exists in the specified directory and installs it if not.
func ensureCosignBinary(ctx context.Context, s *store.Layout) (string, error) {
	l := log.FromContext(ctx)

	// Get the current user's information
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("Error: %v\n", err)
	}

	// Get the user's home directory
	homeDir := currentUser.HomeDir

	// Construct the path to the .hauler directory
	haulerDir := filepath.Join(homeDir, ".hauler")
	
    // Create the .hauler directory if it doesn't exist
    if _, err := os.Stat(haulerDir); os.IsNotExist(err) {
        // .hauler directory does not exist, create it
        if err := os.MkdirAll(haulerDir, 0755); err != nil {
            return "", fmt.Errorf("Error creating .hauler directory: %v\n", err)
        }
        l.Infof("Created .hauler directory at: %s", haulerDir)
    }

	// Check if the cosign binary exists in the specified directory.
    binaryPath := filepath.Join(haulerDir, "cosign")
    _, err = os.Stat(binaryPath)
    if err == nil {
        // Cosign binary is already installed in the specified directory.
        return binaryPath, nil
    }

    // Cosign binary is not found.
    l.Infof("Cosign binary not found. Checking to see if it exists in the store...")

	// grab binary from store if it exists, otherwise try to download it from GitHub.
	// if the binary has to be downloaded, then automatically add it to the store afterwards.
	err = copyCosignFromStore(ctx, s, haulerDir)
	if err != nil {
		l.Warnf("%s", err)
		err = downloadCosign(ctx, haulerDir)
		if err != nil {
			return "", err
		}
		err = addCosignToStore(ctx, s, binaryPath)
		if err != nil {
			return "", err
		}
	}

	return binaryPath, nil
}

// used to check if the cosign binary is in the store and if so copy it to the .hauler directory
func copyCosignFromStore(ctx context.Context, s *store.Layout, destDir string) error {
	l := log.FromContext(ctx)

	ref := "hauler/cosign:latest"
	r, err := reference.Parse(ref)
	if err != nil {
		return err
	}

	found := false
	if err := s.Walk(func(reference string, desc ocispec.Descriptor) error {
	
		if !strings.Contains(reference, r.Name()) {
			return nil
		}
		found = true

		rc, err := s.Fetch(ctx, desc)
		if err != nil {
			return err
		}
		defer rc.Close()

		var m ocispec.Manifest
		if err := json.NewDecoder(rc).Decode(&m); err != nil {
			return err
		}

		mapperStore, err := mapper.FromManifest(m, destDir)
		if err != nil {
			return err
		}

		pushedDesc, err := s.Copy(ctx, reference, mapperStore, "")
		if err != nil {
			return err
		}

		l.Infof("extracted [%s] from store with digest [%s]", ref, pushedDesc.Digest.String())

		return nil
	}); err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("Reference [%s] not found in store.  Hauler will attempt to download it from Github.", ref)
	}

	return nil
}

// adds the cosign binary to the store.
// this is to help with airgapped situations where you cannot access the internet.
func addCosignToStore(ctx context.Context, s *store.Layout, binaryPath string) error {
	l := log.FromContext(ctx)
	
	fi := v1alpha1.File{
		Path: binaryPath,
	}

	copts := getter.ClientOptions{
		NameOverride: fi.Name,
	}

	f := file.NewFile(fi.Path, file.WithClient(getter.NewClient(copts)))
	ref, err := reference.NewTagged(f.Name(fi.Path), reference.DefaultTag)
	if err != nil {
		return err
	}

	desc, err := s.AddOCI(ctx, f, ref.Name())
	if err != nil {
		return err
	}

	l.Infof("added 'file' to store at [%s], with digest [%s]", ref.Name(), desc.Digest.String())
	return nil
}


// used to check if the cosign binary is in the store and if so copy it to the .hauler directory
func downloadCosign(ctx context.Context, haulerDir string) error {
	l := log.FromContext(ctx)

    // Define the GitHub release URL and architecture-specific binary name.
    releaseURL := "https://github.com/rancher-government-solutions/cosign/releases/latest/download"
	
    // Determine the architecture and add it to the binary name.
    arch := runtime.GOARCH
	rOS := runtime.GOOS
	binaryName := "cosign"
	if rOS == "windows" {
		binaryName = fmt.Sprintf("cosign-%s-%s.exe", rOS, arch)
	} else {
		binaryName = fmt.Sprintf("cosign-%s-%s", rOS, arch)
	}
	
    // Download the binary.
    downloadURL := fmt.Sprintf("%s/%s", releaseURL, binaryName)
    resp, err := http.Get(downloadURL)
    if err != nil {
        return fmt.Errorf("error downloading cosign binary: %v", err)
    }
    defer resp.Body.Close()

    // Create the cosign binary file in the specified directory.
    binaryFile, err := os.Create(filepath.Join(haulerDir, binaryName))
    if err != nil {
        return fmt.Errorf("error creating cosign binary: %v", err)
    }
    defer binaryFile.Close()

    // Copy the downloaded binary to the file.
    _, err = io.Copy(binaryFile, resp.Body)
    if err != nil {
        return fmt.Errorf("error saving cosign binary: %v", err)
    }

    // Make the binary executable.
    if err := os.Chmod(binaryFile.Name(), 0755); err != nil {
        return fmt.Errorf("error setting executable permission: %v", err)
    }

    // Rename the binary to "cosign"
	oldBinaryPath := filepath.Join(haulerDir, binaryName)
    newBinaryPath := filepath.Join(haulerDir, "cosign")
    if err := os.Rename(oldBinaryPath, newBinaryPath); err != nil {
        return fmt.Errorf("error renaming cosign binary: %v", err)
    }

    l.Infof("Cosign binary downloaded and installed to %s", haulerDir)
	return nil
}