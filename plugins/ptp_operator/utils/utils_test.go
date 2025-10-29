package utils_test

import (
	"fmt"
	"testing"

	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/utils"
	"github.com/stretchr/testify/assert"
)

type testCase struct {
	ifname                 string
	expectedClockIdentifier string
}

func Test_GetClockIdentifier(t *testing.T) {
	testCases := []testCase{
		// Special clock identifiers (should return as-is)
		{"CLOCK_REALTIME", "CLOCK_REALTIME"},
		{"master", "master"},
		{"/dev/ptp0", "/dev/ptp0"},
		{"/dev/ptp1", "/dev/ptp1"},
		// Fallback cases (interfaces that don't match or have no PHC - will return original)
		{"wlan", "wlan"},
		{"lo", "lo"},
		{"virbr", "virbr"},
		{"docker", "docker"},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s->%s", tc.ifname, tc.expectedClockIdentifier), func(t *testing.T) {
			assert.Equal(t, tc.expectedClockIdentifier, GetClockIdentifier(tc.ifname))
		})
	}
}

// Test_GetClockIdentifier_RealInterfaces tests with real network interfaces
// These tests may return /dev/ptpX if the interface has PHC support,
// or the original name if PHC lookup fails
func Test_GetClockIdentifier_RealInterfaces(t *testing.T) {
	testInterfaces := []string{
		"eth0",
		"eth1",
		"enP2s2f0np0",
		"enP1s1f1np1",
		"ens1f3np3",
	}
	
	for _, iface := range testInterfaces {
		t.Run(iface, func(t *testing.T) {
			result := utils.GetClockIdentifier(iface)
			// Result should either be /dev/ptpX or the original interface name
			if result != iface {
				assert.Regexp(t, `^/dev/ptp\d+$`, result, "If changed, should be /dev/ptpX format")
			}
		})
	}
}

// Test_GetClockIdentifier_VLAN tests VLAN interface handling
// PTP runs on the base interface, so VLAN tags should be stripped
func Test_GetClockIdentifier_VLAN(t *testing.T) {
	testCases := []struct {
		ifname string
	}{
		{ifname: "eth1.100"},
		{ifname: "ens1f0.100"},
		{ifname: "enP2s2f0np0.100"},
		{ifname: "enP1s1f1np1.200"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.ifname, func(t *testing.T) {
			result := utils.GetClockIdentifier(tc.ifname)
			// If PHC lookup succeeded, result should be /dev/ptpX (WITHOUT VLAN tag)
			// If it failed, result should be original name
			if result != tc.ifname {
				assert.Regexp(t, `^/dev/ptp\d+$`, result, "Should be /dev/ptpX format without VLAN tag")
				assert.NotContains(t, result, ".", "VLAN tag should NOT be in PHC ID")
			}
		})
	}
}
