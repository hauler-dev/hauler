package cosign

import (
	"fmt"
	"oras.land/oras-go/pkg/content"
	"os/exec"
)

// VerifyFileSignature verifies the digital signature of a file using Sigstore/Cosign.
func VerifySignature(filePath string, keyPath string) error {
	// Command to verify the signature using Cosign.
	cmd := exec.Command("cosign", "verify", "--insecure-ignore-tlog", "--key", keyPath, filePath)

	// Run the command and capture its output.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error verifying signature: %v, output: %s", err, output)
	}

	return nil
}

// SaveImage saves image and any signatures/attestations to the store.
func SaveImage(storePath string, ref string) error {
	// Command to verify the signature using Cosign.
	cmd := exec.Command("cosign", "save", ref, "--dir", storePath)

	// Run the command and capture its output.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error adding image to store: %v, output: %s", err, output)
	}

	return nil
}

// LoadImage loads store to a remote registry.
func LoadImage(storePath string, registry string, ropts content.RegistryOptions) error {
	// Command to verify the signature using Cosign.
	cmd := exec.Command("cosign", "load", "--registry", registry, "--dir", storePath)

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
