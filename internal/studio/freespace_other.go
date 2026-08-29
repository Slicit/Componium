//go:build !unix

package studio

// freeBytes has no portable implementation, and the studio is meant to run on
// Windows too. Reporting zero is honest: the page treats it as unknown and
// simply does not show a figure, which is better than a wrong one and much
// better than not building.
func freeBytes(dir string) int64 { return 0 }
