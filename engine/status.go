package engine

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Status is the tunnel's liveness data, the same numbers wg show prints.
type Status struct {
	LastHandshake time.Time // zero until the first handshake
	RxBytes       int64
	TxBytes       int64
}

// Status parses the device's UAPI dump. The dump is key=value lines;
// with a single peer the last value for each key is the one that counts.
func (t *Tunnel) Status() (Status, error) {
	dump, err := t.dev.IpcGet()
	if err != nil {
		return Status{}, fmt.Errorf("read device state: %w", err)
	}

	var st Status
	for _, line := range strings.Split(dump, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "last_handshake_time_sec":
			if sec, err := strconv.ParseInt(v, 10, 64); err == nil && sec > 0 {
				st.LastHandshake = time.Unix(sec, 0)
			}
		case "rx_bytes":
			st.RxBytes, _ = strconv.ParseInt(v, 10, 64)
		case "tx_bytes":
			st.TxBytes, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	return st, nil
}
