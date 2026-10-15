//go:build !race

package auth

// raceEnabled reports that the test binary was built with the race detector
// (see the //go:build race counterpart). Normal builds keep the tight bounds.
const raceEnabled = false
