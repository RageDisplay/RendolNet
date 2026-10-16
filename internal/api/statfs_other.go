//go:build !linux

package api

import "errors"

func diskUsage(string) (uint64, uint64, error) { return 0, 0, errors.New("только Linux") }
