package utils

import (
	"sync"
)

// phcCache stores the mapping of interface names to PHC IDs
// This is populated from the NodePtpDevice CR and used as the source of truth
var (
	phcCache     map[string]string
	phcCacheLock sync.RWMutex
)

func init() {
	phcCache = make(map[string]string)
}

// PtpDevice represents a minimal PTP device for cache purposes
// This mirrors the structure from the NodePtpDevice CR
type PtpDevice struct {
	Name  string
	PhcId string
}

// UpdatePhcCacheFromDevices updates the PHC cache from a list of PTP devices
// This should be called whenever the NodePtpDevice CR is retrieved/updated
func UpdatePhcCacheFromDevices(devices []PtpDevice) {
	phcCacheLock.Lock()
	defer phcCacheLock.Unlock()

	// Clear existing cache
	phcCache = make(map[string]string)

	// Populate from device list
	for _, device := range devices {
		if device.Name != "" && device.PhcId != "" {
			phcCache[device.Name] = device.PhcId
		}
	}
}

// UpdatePhcCacheEntry adds or updates a single entry in the PHC cache
func UpdatePhcCacheEntry(ifname, phcId string) {
	if ifname == "" || phcId == "" {
		return
	}

	phcCacheLock.Lock()
	defer phcCacheLock.Unlock()
	phcCache[ifname] = phcId
}

// GetPhcIdFromCache retrieves the PHC ID for an interface from the cache
// Returns empty string if not found
func GetPhcIdFromCache(ifname string) string {
	phcCacheLock.RLock()
	defer phcCacheLock.RUnlock()

	if phcId, found := phcCache[ifname]; found {
		return phcId
	}
	return ""
}

// ClearPhcCache clears the PHC ID cache
func ClearPhcCache() {
	phcCacheLock.Lock()
	defer phcCacheLock.Unlock()
	phcCache = make(map[string]string)
}
