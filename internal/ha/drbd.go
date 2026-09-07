package ha

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// DRBDProof requires a single volume with an explicit quorum indication. A
// diskless witness permits either data node to survive its peer's failure.
func DRBDProof(resource, volume, device string, status, mount []byte) (Proof, error) {
	var resources []struct {
		Name      string `json:"name"`
		Role      string `json:"role"`
		FailIO    bool   `json:"force-io-failures"`
		Suspended bool   `json:"suspended"`
		Devices   []struct {
			Minor  *int   `json:"minor"`
			Disk   string `json:"disk-state"`
			Quorum bool   `json:"quorum"`
		} `json:"devices"`
		Connections []struct {
			State   string `json:"connection-state"`
			Role    string `json:"peer-role"`
			Devices []struct {
				Disk string `json:"peer-disk-state"`
			} `json:"peer_devices"`
		} `json:"connections"`
	}
	if e := json.Unmarshal(status, &resources); e != nil {
		return Proof{}, e
	}
	var mounts struct {
		Filesystems []struct {
			Target  string `json:"target"`
			Source  string `json:"source"`
			Options string `json:"options"`
		} `json:"filesystems"`
	}
	if e := json.Unmarshal(mount, &mounts); e != nil {
		return Proof{}, e
	}
	proof := Proof{Volume: volume}
	found := false
	for _, r := range resources {
		if r.Name != resource {
			continue
		}
		if found || len(r.Devices) != 1 {
			return proof, fmt.Errorf("ambiguous DRBD resource")
		}
		found = true
		if r.Devices[0].Minor == nil || device != fmt.Sprintf("/dev/drbd%d", *r.Devices[0].Minor) {
			return proof, fmt.Errorf("DRBD resource does not own configured device")
		}
		proof.Primary = r.Role == "Primary" && !r.Suspended && !r.FailIO
		proof.UpToDate = r.Devices[0].Disk == "UpToDate"
		proof.Quorum = r.Devices[0].Quorum
		for _, c := range r.Connections {
			if c.Role == "Primary" {
				return proof, fmt.Errorf("multiple primary owners")
			}
			for _, d := range c.Devices {
				proof.PeerUpToDate = proof.PeerUpToDate || (c.State == "Connected" && d.Disk == "UpToDate")
			}
		}
	}
	for _, m := range mounts.Filesystems {
		if filepath.Clean(m.Target) == filepath.Clean(volume) && filepath.Clean(m.Source) == filepath.Clean(device) && strings.Contains(","+m.Options+",", ",rw,") {
			proof.Mounted = true
		}
	}
	if !found || !proof.Primary || !proof.UpToDate || !proof.Quorum || !proof.Mounted {
		return proof, fmt.Errorf("unsafe DRBD ownership or mount")
	}
	return proof, nil
}
