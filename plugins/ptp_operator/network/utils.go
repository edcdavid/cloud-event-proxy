package network

import (
	"fmt"
	"os/exec"
	"regexp"
)

// GetPhcId returns the PHC device identifier (/dev/ptpX) for a given network interface.
// It uses ethtool to query the PTP Hardware Clock index.
//
// Parameters:
//   - iface: Network interface name (e.g., "ens1f0", "eth0")
//
// Returns:
//   - PHC device path (e.g., "/dev/ptp0") or empty string if not found
func GetPhcId(iface string) string {
	var err error
	var id int
	if id, err = getPTPClockIndex(iface); err != nil {
		// Interface doesn't have PHC support or ethtool failed
		return ""
	}
	return fmt.Sprintf("/dev/ptp%d", id)
}

func getPTPClockIndex(iface string) (int, error) {
	if !ethtoolInstalled() {
		return 0, fmt.Errorf("ethtool not installed")
	}

	// Command to get PTP clock info
	cmd := exec.Command("ethtool", "-T", iface)

	// Execute the command and capture output
	out, err := cmd.CombinedOutput()
	if err != nil {
		return -1, fmt.Errorf("failed to run ethtool on %s: %w", iface, err)
	}

	// Regex to extract clock index
	re := regexp.MustCompile(`PTP Hardware Clock: (\d+)`)
	match := re.FindSubmatch(out)
	if match == nil {
		return -1, fmt.Errorf("no PTP hardware clock found on %s", iface)
	}
	
	// Convert captured index string to integer
	var clockIndex int
	_, err = fmt.Sscanln(string(match[1]), &clockIndex)
	if err != nil {
		return 0, err
	}
	return clockIndex, nil
}

func ethtoolInstalled() bool {
	_, err := exec.LookPath("ethtool")
	return err == nil
}

