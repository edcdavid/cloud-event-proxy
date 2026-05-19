package metrics_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/alias"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/config"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/metrics"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/ptp4lconf"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/stats"
	"github.com/redhat-cne/cloud-event-proxy/plugins/ptp_operator/types"
	"github.com/redhat-cne/sdk-go/pkg/event/ptp"
	"github.com/stretchr/testify/assert"
)

// TestOCPBUGS85092_ClockStateStuckFreerunAfterDualPortFaulty reproduces
// OCPBUGS-85092: on OC 2-port configs, when both ports go FAULTY during a
// cloud-event-proxy restart re-emit cycle, the holdover timer expires and
// sets ens3fx clock_state to FREERUN. After that, subsequent ptp4l
// "master offset ... s2" log lines fail to transition the metric back to
// LOCKED because ptp4lCfg.ByRole(SLAVE) returns no interface (both are
// FAULTY), causing the metric update to be skipped entirely.
func TestOCPBUGS85092_ClockStateStuckFreerunAfterDualPortFaulty(t *testing.T) {
	// --- Setup: OC 2-port config with ens3f2 (SLAVE) + ens3f3 (LISTENING) ---
	alias.SetAlias("ens3f2", "ens3fx")
	alias.SetAlias("ens3f3", "ens3fx")

	oc2PortConfig := &ptp4lconf.PTP4lConfig{
		Name:    "ptp4l.0.config",
		Profile: "ordinary-2-port",
		Interfaces: []*ptp4lconf.PTPInterface{
			{
				Name:     "ens3f2",
				PortID:   1,
				PortName: "port 1",
				Role:     types.SLAVE,
			},
			{
				Name:     "ens3f3",
				PortID:   2,
				PortName: "port 2",
				Role:     types.LISTENING,
			},
		},
		Sections: map[string]map[string]string{
			"ens3f2": {"masterOnly": "0"},
			"ens3f3": {"masterOnly": "0"},
			"global": {"slaveOnly": "1"},
		},
	}

	mockFS := &metrics.MockFileSystem{}
	sc := &common.SCConfiguration{StorePath: "/tmp/store"}
	metrics.Filesystem = mockFS
	pem := metrics.NewPTPEventManager("", InitPubSubTypes(), MYNODE, sc)
	pem.MockTest(true)
	pem.AddPTPConfig(types.ConfigName(oc2PortConfig.Name), oc2PortConfig)

	// Master stats: ptp4l is the offset source, initially LOCKED
	statsMaster := stats.NewStats(oc2PortConfig.Name)
	statsMaster.SetOffsetSource("master")
	statsMaster.SetProcessName("ptp4l")
	statsMaster.SetAlias("ens3fx")
	statsMaster.SetLastSyncState(ptp.LOCKED)
	statsMaster.SetRole(types.SLAVE)
	statsMaster.AddValue(4)

	// CLOCK_REALTIME stats
	statsRT := stats.NewStats(oc2PortConfig.Name)
	statsRT.SetOffsetSource("phc")
	statsRT.SetProcessName("phc2sys")
	statsRT.SetLastSyncState(ptp.LOCKED)

	pem.Stats[types.ConfigName(oc2PortConfig.Name)] = make(stats.PTPStats)
	pem.Stats[types.ConfigName(oc2PortConfig.Name)][types.IFace("master")] = statsMaster
	pem.Stats[types.ConfigName(oc2PortConfig.Name)][types.IFace("CLOCK_REALTIME")] = statsRT

	pem.PtpConfigMapUpdates = config.NewLinuxPTPConfUpdate()
	pem.PtpConfigMapUpdates.EventThreshold["ordinary-2-port"] = &config.PtpClockThreshold{
		HoldOverTimeout:    5,
		MaxOffsetThreshold: 150,
		MinOffsetThreshold: -150,
		Close:              make(chan struct{}),
	}
	pem.PtpConfigMapUpdates.PtpProcessOpts["ordinary-2-port"] = &config.PtpProcessOpts{}

	metrics.RegisterMetrics(MYNODE)
	metrics.SetMasterOffsetSource("ptp4l")

	// Verify initial LOCKED state: feed a normal "master offset" line
	pem.ExtractMetrics("ptp4l[4488.660]: [ptp4l.0.config] master offset          1 s2 freq     +25 path delay       208")

	syncState := metrics.SyncState.With(map[string]string{
		"process": "ptp4l", "node": MYNODE, "iface": "ens3fx",
	})
	assert.Equal(t, float64(types.LOCKED), testutil.ToFloat64(syncState),
		"precondition: clock_state should be LOCKED before FAULTY sequence")

	// --- Reproduce: both ports go FAULTY (simulating re-emit of old log lines) ---
	pem.ExtractMetrics("ptp4l[4488.347]: [ptp4l.0.config:5] port 2 (ens3f3): LISTENING to UNCALIBRATED on RS_SLAVE")
	pem.ExtractMetrics("ptp4l[4488.349]: [ptp4l.0.config:5] port 1 (ens3f2): SLAVE to FAULTY on FAULT_DETECTED (FT_UNSPECIFIED)")

	// After the SLAVE port goes FAULTY, the state machine enters HOLDOVER
	masterStats := pem.GetStatsForInterface(types.ConfigName(oc2PortConfig.Name), "master")
	assert.Equal(t, ptp.HOLDOVER, masterStats.LastSyncState(),
		"after SLAVE port goes FAULTY, master should be in HOLDOVER")

	// Simulate holdover timeout expiring (sets state to FREERUN)
	masterStats.SetLastSyncState(ptp.FREERUN)
	metrics.UpdateSyncStateMetrics("ptp4l", "ens3fx", ptp.FREERUN)

	syncState = metrics.SyncState.With(map[string]string{
		"process": "ptp4l", "node": MYNODE, "iface": "ens3fx",
	})
	assert.Equal(t, float64(types.FREERUN), testutil.ToFloat64(syncState),
		"after holdover timeout, clock_state should be FREERUN")

	// Verify that no port has SLAVE role (both are FAULTY/UNCALIBRATED)
	_, err := oc2PortConfig.ByRole(types.SLAVE)
	assert.Error(t, err, "no port should have SLAVE role after both went FAULTY")

	// --- Bug trigger: ptp4l sends "master offset ... s2" (it has recovered) ---
	// But cloud-event-proxy can't map it to the alias because ByRole(SLAVE) fails
	pem.ExtractMetrics("ptp4l[4753.914]: [ptp4l.0.config] master offset          4 s2 freq      +2 path delay       200")

	// --- Assertion: after ptp4l reports s2, clock_state MUST return to LOCKED ---
	syncState = metrics.SyncState.With(map[string]string{
		"process": "ptp4l", "node": MYNODE, "iface": "ens3fx",
	})
	currentState := testutil.ToFloat64(syncState)

	// OCPBUGS-85092: this fails while the bug is present because
	// ptp4lCfg.ByRole(SLAVE) returns no interface when both ports are FAULTY,
	// causing the metric update to be skipped entirely.
	assert.Equal(t, float64(types.LOCKED), currentState,
		"OCPBUGS-85092: clock_state must return to LOCKED when ptp4l reports s2, "+
			"but stays FREERUN because ByRole(SLAVE) fails when both ports are FAULTY")
}
