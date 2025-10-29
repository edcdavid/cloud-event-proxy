package utils

import (
	"strings"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/network"
)

// GetClockIdentifier generates a PTP clock identifier from network interface names.
// For PHC-capable network interfaces, it returns the native /dev/ptpX identifier.
// For other clock types (CLOCK_REALTIME, master, etc.), it returns the name unchanged.
//
// This function first checks the PHC ID cache (populated from NodePtpDevice CR) as the
// primary source of truth. If not found in cache, it falls back to querying via ethtool.
//
// Parameters:
//   - ifname: Network interface name (e.g., "ens1f0", "enP2s2f0np0") or clock identifier
//
// Returns:
//   - /dev/ptpX for PHC-capable network interfaces, or original name for other clock types
func GetClockIdentifier(ifname string) string {
	if ifname == "" {
		return ""
	}

	// For special clock identifiers (not actual network interfaces), return as-is
	if ifname == "CLOCK_REALTIME" || ifname == "master" || strings.HasPrefix(ifname, "/dev/ptp") {
		return ifname
	}

	// For network interfaces, get the PHC device identifier
	// Strip VLAN tag if present (e.g., ens1f0.100 -> ens1f0)
	// PTP runs on the base interface, not per VLAN
	baseIface := ifname
	if idx := strings.Index(ifname, "."); idx != -1 {
		baseIface = ifname[:idx]
	}

	// First, try to get PHC ID from cache (populated from NodePtpDevice CR)
	// This is the preferred source of truth
	phcId := GetPhcIdFromCache(baseIface)

	// If not in cache, fall back to querying via ethtool
	if phcId == "" {
		phcId = network.GetPhcId(baseIface)
	}

	// If we have a PHC ID, return it (without VLAN tag)
	// PTP hardware clock is shared across all VLANs on the same interface
	if phcId != "" {
		return phcId
	}

	// If PHC ID cannot be determined, return the original interface name
	return ifname
}
