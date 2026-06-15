//go:build !linux

package conntrack

// platformGet returns a Counts map filled with zeros on non-Linux
// systems. This keeps the API usable during Windows / macOS dev runs
// even though established-conn counting is not implemented there.
func platformGet(ports []uint16) (Counts, error) {
	out := make(Counts, len(ports))
	for _, p := range ports {
		out[p] = 0
	}
	return out, nil
}
