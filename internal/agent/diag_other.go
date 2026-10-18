//go:build !linux

package agent

import "context"

func diagPing(context.Context, string, string, int, []string) (*PingResult, error) {
	return nil, errNotLinux
}
func diagTrace(context.Context, string, int, []string) (*TraceResult, error) { return nil, errNotLinux }
func diagRoutes() ([]RouteEntry, error)                                      { return nil, errNotLinux }
func diagNeighbors() ([]Neighbor, error)                                     { return nil, errNotLinux }
func diagLookup(context.Context, string, string) (*LookupResult, error)      { return nil, errNotLinux }
