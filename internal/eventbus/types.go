package eventbus

import "time"

// EventType is a short stable identifier for each event variant.
type EventType string

const (
	TypeInstanceState    EventType = "instance.state"
	TypeInstanceError    EventType = "instance.error"
	TypeProxyStatus      EventType = "proxy.status"
	TypeProxyConnections EventType = "proxy.connections"
	TypeConfigChanged    EventType = "config.changed"
	TypeConfigDeleted    EventType = "config.deleted"
	TypeLogLine          EventType = "log.line"
	// TypeBackupRun fires when a scheduled/manual backup starts or finishes; the
	// payload is the backup run record so the UI can live-update its history.
	TypeBackupRun EventType = "backup.run"

	// Network-config change events (KWRT DHCP / static-routing manager). The
	// payload is NetChangeData; the frontend refreshes the affected list.
	TypeDHCPChanged   EventType = "dhcp.changed"
	TypeStaticChanged EventType = "static.changed"
	TypeLeaseChanged  EventType = "lease.changed"
	TypeACLChanged    EventType = "acl.changed"
	TypeRouteChanged  EventType = "route.changed"
	TypeIfaceChanged  EventType = "iface.changed"
	TypeIPv6Changed   EventType = "ipv6.changed"
)

// NetChangeData is the payload for the network-config change events. Action is
// "create" | "update" | "delete" | "toggle" | "apply"; Count is the resulting
// item count where meaningful.
type NetChangeData struct {
	Action string `json:"action,omitempty"`
	Count  int    `json:"count,omitempty"`
}

// Event is a single message published on the bus. Data is the type-
// specific payload; subscribers may inspect Type to decide how to
// decode it.
type Event struct {
	Seq      uint64    `json:"seq"`
	Type     EventType `json:"type"`
	ConfigID string    `json:"config_id,omitempty"`
	TS       time.Time `json:"ts"`
	Data     any       `json:"data,omitempty"`
}

// InstanceStateData is the payload for TypeInstanceState.
type InstanceStateData struct {
	State     string `json:"state"`
	PrevState string `json:"prev_state,omitempty"`
}

// InstanceErrorData is the payload for TypeInstanceError.
type InstanceErrorData struct {
	Message string `json:"message"`
}

// ProxyStatusData is the payload for TypeProxyStatus.
type ProxyStatusData struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	RemoteAddr string `json:"remote_addr,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ProxyConnectionsData is the payload for TypeProxyConnections.
type ProxyConnectionsData struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	CurConns int    `json:"cur_conns"`
}

// LogLineData is the payload for TypeLogLine.
type LogLineData struct {
	Line string `json:"line"`
}
