// Package conntrack reports the number of currently established TCP
// connections owned by the daemon, grouped by local port. On Linux it
// reads /proc/net/tcp{,6}. On other platforms it returns zeros so the
// rest of the API still works during local development.
package conntrack

// Counts maps local port → established connection count.
type Counts map[uint16]int

// Get returns established counts for any of `ports`. Ports not present
// in the map mean "zero established right now".
func Get(ports []uint16) (Counts, error) {
	return platformGet(ports)
}
