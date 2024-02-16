package cosign

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"context"
	"time"
	"bufio"
	"embed"
	"strings"

	"oras.land/oras-go/pkg/content"
	"github.com/rancherfederal/hauler/pkg/store"
	"github.com/rancherfederal/hauler/pkg/log"
)

const maxRetries = 3
const retryDelay = time.Second * 5

// VerifyFileSignature verifies the digital signature of a file using Sigstore/Cosign.
func VerifySignature(ctx context.Context, s *store.Layout, keyPath string, ref string) error {
	operation := func() error {
		cosignBinaryPath, err := getCosignPath(ctx)
		if err != nil {
			return err
		}

		cmd := exec.Command(cosignBinaryPath, "verify", "--insecure-ignore-tlog", "--key", keyPath, ref)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("error verifying signature: %v, output: %s", err, output)
		}

		return nil
	}

	return RetryOperation(ctx, operation)
}

// SaveImage saves image and any signatures/attestations to the store.
func SaveImage(ctx context.Context, s *store.Layout, ref string, platform string) error {
	l := log.FromContext(ctx)
	operation := func() error {
		cosignBinaryPath, err := getCosignPath(ctx)
		if err != nil {
			return err
		}

		cmd := exec.Command(cosignBinaryPath, "save", ref, "--dir", s.Root)
		// Conditionally add platform.
		if platform != "" {
			cmd.Args = append(cmd.Args, "--platform", platform)
		}
		
		output, err := cmd.CombinedOutput()
		if err != nil {
			if strings.Contains(string(output), "specified reference is not a multiarch image") {
				l.Debugf(fmt.Sprintf("specified image [%s] is not a multiarch image.  (choosing default)", ref))
				// Rerun the command without the platform flag
				cmd = exec.Command(cosignBinaryPath, "save", ref, "--dir", s.Root)
				output, err = cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("error adding image to store: %v, output: %s", err, output)
				}
			} else {
				return fmt.Errorf("error adding image to store: %v, output: %s", err, output)
			}
		}

		return nil
	}

	return RetryOperation(ctx, operation)
}

// LoadImage loads store to a remote registry.
func LoadImages(ctx context.Context, s *store.Layout, registry string, ropts content.RegistryOptions) error {
	l := log.FromContext(ctx)

	cosignBinaryPath, err := getCosignPath(ctx)
	if err != nil {
		return err
	}

	cmd := exec.Command(cosignBinaryPath, "load", "--registry", registry, "--dir", s.Root)

	// Conditionally add extra registry flags.
	if ropts.Insecure {
		cmd.Args = append(cmd.Args, "--allow-insecure-registry=true")
	}
	if ropts.PlainHTTP {
		cmd.Args = append(cmd.Args, "--allow-http-registry=true")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	// start the command after having set up the pipe
	if err := cmd.Start(); err != nil {
		return err
	}
	
	// read command's stdout line by line
	output := bufio.NewScanner(stdout)
	for output.Scan() {
		l.Infof(output.Text()) // write each line to your log, or anything you need
	}
	if err := output.Err(); err != nil {
		cmd.Wait()
		return err
	}

	// read command's stderr line by line
	errors := bufio.NewScanner(stderr)
	for errors.Scan() {
		l.Errorf(errors.Text()) // write each line to your log, or anything you need
	}
	if err := errors.Err(); err != nil {
		cmd.Wait()
		return err
	}

	// Wait for the command to finish
	err = cmd.Wait()
	if err != nil {
		return err
	}

	return nil
}

// RegistryLogin - performs cosign login
func RegistryLogin(ctx context.Context, s *store.Layout, registry string, ropts content.RegistryOptions) error {
	log := log.FromContext(ctx)
	cosignBinaryPath, err := getCosignPath(ctx)
	if err != nil {
		return err
	}

	cmd := exec.Command(cosignBinaryPath, "login", registry, "-u", ropts.Username, "-p", ropts.Password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error logging into registry: %v, output: %s", err, output)
	}
	log.Infof(strings.Trim(string(output), "\n"))

	return nil
}

func RetryOperation(ctx context.Context, operation func() error) error {
	l := log.FromContext(ctx)
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := operation()
		if err == nil {
			// If the operation succeeds, return nil (no error).
			return nil
		}

		// Log the error for the current attempt.
		l.Errorf("Error (attempt %d/%d): %v", attempt, maxRetries, err)

		// If this is not the last attempt, wait before retrying.
		if attempt < maxRetries {
			time.Sleep(retryDelay)
		}
	}

	// If all attempts fail, return an error.
	return fmt.Errorf("operation failed after %d attempts", maxRetries)
}


func EnsureBinaryExists(ctx context.Context, bin embed.FS) (error) {
	// Set up a path for the binary to be copied.
    binaryPath, err := getCosignPath(ctx)
	if err != nil {
		return fmt.Errorf("Error: %v\n", err)
	}
	
	// Determine the architecture so that we pull the correct embedded binary.
	arch := runtime.GOARCH
	rOS := runtime.GOOS
	binaryName := "cosign"
	if rOS == "windows" {
		binaryName = fmt.Sprintf("cosign-%s-%s.exe", rOS, arch)
	} else {
		binaryName = fmt.Sprintf("cosign-%s-%s", rOS, arch)
	}

	// retrieve the embedded binary
	f, err := bin.ReadFile(fmt.Sprintf("binaries/%s", binaryName))
	if err != nil {
		return fmt.Errorf("Error: %v\n", err)
	}

	// write the binary to the filesystem
	err = os.WriteFile(binaryPath, f, 0755)
	if err != nil {
		return fmt.Errorf("Error: %v\n", err)
	}

	return nil
}


// getCosignPath returns the binary path
func getCosignPath(ctx context.Context) (string, error) {
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
    }

	// Determine the binary name.
	rOS := runtime.GOOS
	binaryName := "cosign"
	if rOS == "windows" {
		binaryName = "cosign.exe"
	}

	// construct path to binary
    binaryPath := filepath.Join(haulerDir, binaryName)

	return binaryPath, nil
}