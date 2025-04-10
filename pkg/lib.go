package pkg

import (
	"fmt"
	"os"
	"os/exec"

	"example.poc/device-monitoring-system/internal/config"
)

func ExecuteExternalChecksumGenerator(arg ...string) ([]byte, error) {
	loc := config.ExternalChecksumGeneratorLocation()
	if loc == "" {
		return nil, fmt.Errorf("environment var EXTERNAL_CHECKSUM_GENERATOR_LOCATION is not set")
	}

	if _, err := os.Stat(loc); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("external checksum generator binary not found at location %s", loc)
		}
		return nil, fmt.Errorf("error checking for external checksum generator binary location: %w", err)
	}

	cmd := exec.Command(loc, arg...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error executing external checksum generator: %w", err)
	}

	return output, nil
}
