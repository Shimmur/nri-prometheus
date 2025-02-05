// Package endpoints ...
// Copyright 2019 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package endpoints

import (
	"fmt"
)

const selfEndpoint = "localhost:8080"
const selfDescription = "nri-prometheus"

type selfRetriever struct {
	targets []Target
}

func newSelfTargetConfig() TargetConfig {
	return TargetConfig{
		Description: selfDescription,
		URLs:        []TargetURL{{URL: selfEndpoint}},
	}
}

// SelfRetriever creates a TargetRetriver that returns the targets belonging
// to nri-prometheus.
func SelfRetriever() (TargetRetriever, error) {
	targets, err := EndpointToTarget(newSelfTargetConfig())
	if err != nil {
		return nil, fmt.Errorf("parsing target %v: %v", selfDescription, err.Error())
	}
	return &selfRetriever{targets: targets}, nil
}

func (f selfRetriever) GetTargets() ([]Target, error) {
	return f.targets, nil
}

func (f selfRetriever) Watch() error {
	// NOOP
	return nil
}

func (f selfRetriever) Name() string {
	return "self"
}
