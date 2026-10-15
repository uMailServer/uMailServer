//go:build race

package auth

// raceEnabled reports that the test binary was built with the race detector.
// Detector instrumentation adds a documented 5-20x constant factor to every
// operation, so wall-clock bounds calibrated for normal builds must be
// relaxed here to stay deterministic under load.
const raceEnabled = true
