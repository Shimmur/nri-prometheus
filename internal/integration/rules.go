// Package integration ...
// Copyright 2019 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package integration

import (
	"fmt"
	"strings"

	"github.com/newrelic/nri-prometheus/internal/pkg/labels"
)

// ProcessingRule is a bundle of multiple rules of different types to
// be applied to metrics.
type ProcessingRule struct {
	Description      string
	AddAttributes    []AddAttributesRule  `mapstructure:"add_attributes"`
	RenameAttributes []RenameRule         `mapstructure:"rename_attributes"`
	RenameMetrics    []RenameMetricRule   `mapstructure:"rename_metrics"`
	IgnoreMetrics    []IgnoreRule         `mapstructure:"ignore_metrics"`
	CopyAttributes   []CopyAttributesRule `mapstructure:"copy_attributes"`
}

// RenameRule is a rule for changing the name of attributes of metrics that
// match the MetricPrefix. When a metric matches, the attributes which match
// any of the keys of Attributes will be renamed to the value in the map.
type RenameRule struct {
	MetricPrefix string                 `mapstructure:"metric_prefix"`
	Attributes   map[string]interface{} `mapstructure:"attributes"`
}

// IgnoreRule skips for processing metrics that match any of the Prefixes.
// Metrics that match any of the Except are never skipped.
// If Prefixes is empty and Except is not, then all metrics that do not
// match Except will be skipped.
type IgnoreRule struct {
	Prefixes []string `mapstructure:"prefixes"`
	Except   []string `mapstructure:"except"`
}

// CopyAttributesRule is a rule that copies the Attributes from the metric that
// matches FromMetric to the metrics that matches (as prefix) with ToMetrics
// only if both have the same values for all the labels defined in MatchBy.
type CopyAttributesRule struct {
	FromMetric string   `mapstructure:"from_metric"`
	ToMetrics  []string `mapstructure:"to_metrics"`
	MatchBy    []string `mapstructure:"match_by"`
	Attributes []string `mapstructure:"attributes"`
}

// AddAttributesRule adds the Attributes to the metrics that match with
// MetricPrefix.
type AddAttributesRule struct {
	MetricPrefix string                 `mapstructure:"metric_prefix"`
	Attributes   map[string]interface{} `mapstructure:"attributes"`
}

// A RenameMetricRule defines a rule to allow a metric to have its name
// changed
type RenameMetricRule struct {
	FromMetric string `mapstructure:"from_metric"`
	ToMetric   string `mapstructure:"to_metric"`
}

// AutoDecorateLabels mixes automatically all the "_info" labels within the other metrics, when correspond, according to
// the following rules:
// - For each "non-info" metric:
//   1. Check the largest label set whose label names coincide with any of the infos.
//   2. If the label set coinciding by name, also coincide by value, all the labels from the "info" will be added to the metric.
//
// - The added labels will be suffixed by the name of the info_metric (e.g. version.nginx_info)
// - If the intersection of label names is an empty set, it is counted as coincidence and all the labels from the "info" will be added to the metric.
// - If the labels coincide with more than a same info metric, we don't do join because we assume they are not vinculating. For example:
//
//     stuff_info{os="linux", version="1.2.3", id="12345"} 1
//     stuff_info{os="linux", version="3.3.3", id="4432"} 1
//     stuff_metric{os="linux"} 3
//
//     Result: Stuff metric won't have added metrics
//
// - If the labels coincide with diverse info metrics, we can add them because they will be suffixed differently:
//
//     stuff_info{os="linux", version="1.2.3", id="12345"} 1
//     thing_info{os="linux", version="3.3.3", id="4432"} 1
//     stuff_metric{os="linux"} 3
//
//     Result: Stuff metric will be exported as:
//     stuff_metric{os="linux", version.stuff_info="1.2.3", id.stuff_info="12345", version.thing_info="3.3.3", id.thing_info="4432"}
//
func AutoDecorateLabels(targetMetrics *TargetMetrics) {
	// Get all the labels from the _info metrics
	infos := make([]labels.InfoSource, 0)
	for _, metric := range targetMetrics.Metrics {
		if strings.HasSuffix(metric.name, "_info") {
			infos = append(infos, labels.InfoSource{
				Name:   metric.name,
				Labels: metric.attributes,
			})
		}
	}

	// For any other non-info metric, try to consolidate the info labels, when apply
	for _, metric := range targetMetrics.Metrics {
		if !strings.HasSuffix(metric.name, "_info") {
			labels.Accumulate(metric.attributes, labels.ToAdd(infos, metric.attributes))
		}
	}
}

// DecorateRule specifies a label decoration rule: a Source metric may decorate a set of Dest metrics if they have in common
// the labels that are named in the Join keyset
type DecorateRule struct {
	Source     string     // source metric name
	Dest       []string   // destination metrics names
	Join       labels.Set // Join labels: values of this set are ignored, it's only to mark the label names
	Attributes labels.Set // Only attributes here will be copied. If empty: all the attributes are copied
}

// CopyAttributes decorate the labels of an entity
func CopyAttributes(targetMetrics *TargetMetrics, rules []DecorateRule) {

	// Fast path, quickly exit if there are no rules defined.
	if len(rules) == 0 {
		return
	}

	dc := MatchingDecorate(targetMetrics, rules)
	for _, metrics := range targetMetrics.Metrics {
		// Gets the decoration rules where the entity is "destination" of labels
		dstRules, ok := dc.Dests[metrics.name]
		if !ok {
			continue
		}
		for _, rule := range dstRules {
			srcAllLabels := dc.SourceLabels[rule.Source]
			for _, srcLabels := range srcAllLabels {
				if toAdd, ok := labels.Join(srcLabels, metrics.attributes, rule.Join); ok {
					if len(rule.Attributes) > 0 {
						labels.AccumulateOnly(metrics.attributes, toAdd, rule.Attributes)
					} else {
						labels.Accumulate(metrics.attributes, toAdd)
					}
				}
			}
		}
	}
}

// DecorationMap is an intermediate rules representation that allows accessing in hashtable-complexity from destination
// metrics to the source metrics that may decorate them
type DecorationMap struct {
	Dests        map[string][]DecorateRule // Set of rules that have as destination the metric named as the key
	SourceLabels map[string][]labels.Set   // For a given source metric names, the label set from all the found entries
}

// MatchingDecorate return the rules that may be applied to the entity, because this entity data contains at last one
// metric whose name coincides with entity and another metric whose name coincide with one of the destinations.
func MatchingDecorate(targetMetrics *TargetMetrics, rules []DecorateRule) DecorationMap {
	dc := DecorationMap{
		Dests:        map[string][]DecorateRule{},
		SourceLabels: map[string][]labels.Set{},
	}

	sources := map[string][]DecorateRule{}

	// Maps all the source and destination entries to their belonging rules
	for i := range rules {
		for _, destPrefix := range rules[i].Dest {

			duplicatedMetrics := map[string]interface{}{} // avoids adding twice the same rule to the same metric

			// this iteration level allows decorate based on prefix
			for _, m := range targetMetrics.Metrics {
				if _, ok := duplicatedMetrics[m.name]; !ok {
					duplicatedMetrics[m.name] = true
					if strings.HasPrefix(m.name, destPrefix) {
						appendDecorate(dc.Dests, m.name, rules[i])
					}
				}
			}
		}
		appendDecorate(sources, rules[i].Source, rules[i])
	}

	// Caches the labels from all the metrics that are marked as source
	for i := range targetMetrics.Metrics {
		if _, ok := sources[targetMetrics.Metrics[i].name]; ok {
			appendLabels(dc.SourceLabels, targetMetrics.Metrics[i].name, targetMetrics.Metrics[i].attributes)
		}
	}

	return dc
}

// appends a rule to the map with a given key, creating or updating the slice when necessary
func appendDecorate(m map[string][]DecorateRule, key string, r DecorateRule) {
	var rs []DecorateRule
	var ok bool
	if rs, ok = m[key]; !ok {
		rs = make([]DecorateRule, 0)
		m[key] = rs
	}
	m[key] = append(rs, r)
}

// appends a label Set to the map with a given key, creating or updating the slice when necessary
func appendLabels(m map[string][]labels.Set, key string, ls labels.Set) {
	var la []labels.Set
	var ok bool
	if la, ok = m[key]; !ok {
		la = make([]labels.Set, 0)
		m[key] = la
	}
	m[key] = append(la, ls)
}

// Decorate merges the entity and metrics metadata into each metric label
func Decorate(targetMetrics *TargetMetrics, decorateRules []DecorateRule) {
	CopyAttributes(targetMetrics, decorateRules)
	for mi := range targetMetrics.Metrics {
		labels.Accumulate(targetMetrics.Metrics[mi].attributes, targetMetrics.Target.Metadata())
	}
}

// Rename apply the given rename rules to the entities metrics
func Rename(targetMetrics *TargetMetrics, rules []RenameRule) {

	// Fast path, quickly exit if there are no rules defined.
	if len(rules) == 0 {
		return
	}

	for mi := range targetMetrics.Metrics {
		// processing rules into it
		for _, rr := range rules {
			if strings.HasPrefix(targetMetrics.Metrics[mi].name, rr.MetricPrefix) {
				for current, updated := range rr.Attributes {
					if value, ok := targetMetrics.Metrics[mi].attributes[current]; ok {
						targetMetrics.Metrics[mi].attributes[updated.(string)] = value
					}
				}
			}
		}
	}
}

// RenameMetrics will transform the name of a metric, not the attributes
func RenameMetrics(targetMetrics *TargetMetrics, rules []RenameMetricRule) {
	for mi := range targetMetrics.Metrics {
		// processing rules into it
		for _, rr := range rules {
			// We must rename to something, otherwise move along
			if rr.ToMetric == "" {
				continue
			}

			if targetMetrics.Metrics[mi].name == rr.FromMetric {
				targetMetrics.Metrics[mi].name = rr.ToMetric
			}
		}
	}
}

// AddAttributes applies the AddAttributeRule. It adds the attributes defined
// in the rules to the metrics that match.
func AddAttributes(targetMetrics *TargetMetrics, rules []AddAttributesRule) {

	// Fast path, quickly exit if there are no rules defined.
	if len(rules) == 0 {
		return
	}

	for mi := range targetMetrics.Metrics {
		for _, rr := range rules {
			if strings.HasPrefix(targetMetrics.Metrics[mi].name, rr.MetricPrefix) {
				labels.Accumulate(targetMetrics.Metrics[mi].attributes, rr.Attributes)
			}
		}
	}
}

type ignoreRules []IgnoreRule

func (rules ignoreRules) shouldIgnore(name string) bool {
	var prefixesLen, exceptRulesLen int
	for _, rule := range rules {
		exceptRulesLen += len(rule.Except)
		for _, prefix := range rule.Except {
			if strings.HasPrefix(name, prefix) {
				return false
			}
		}

		prefixesLen += len(rule.Prefixes)
		for _, prefix := range rule.Prefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}

	if prefixesLen > 0 {
		return false
	}

	// only exceptions were provided and the current metric is not an exception
	return exceptRulesLen > 0
}

// Filter removes the metrics whose name matches the prefixes in the given ignore rules
func Filter(targetMetrics *TargetMetrics, rules ignoreRules) {

	// Fast path, quickly exit if there are no rules defined.
	if len(rules) == 0 {
		return
	}

	copied := make([]Metric, 0, len(targetMetrics.Metrics))
	for _, m := range targetMetrics.Metrics {
		if !rules.shouldIgnore(m.name) {
			copied = append(copied, m)
		}
	}
	targetMetrics.Metrics = copied
}

// ReNamespaceMetrics will transform the name of a metric, prepending a metrics namespace
// as configured for the URL they were fetched from.
func ReNamespaceMetrics(targetMetrics *TargetMetrics) {
	for mi := range targetMetrics.Metrics {
		if targetMetrics.Target.MetricNamespace != "" {
			targetMetrics.Metrics[mi].name = fmt.Sprintf(
				"%s.%s",
				targetMetrics.Target.MetricNamespace,
				targetMetrics.Metrics[mi].name,
			)
		}
	}
}

// A Processor is something that transform the metrics of a target that are received by a channel, and submits them
// by another channel
type Processor func(pairs <-chan TargetMetrics) <-chan TargetMetrics

// RuleProcessor process apply the Rename, Decorate and Filter metrics
// processing and returns them through a channel.
func RuleProcessor(processingRules []ProcessingRule, queueLength int) Processor {
	var renameRules []RenameRule
	var renameMetricRules []RenameMetricRule
	var ignoreRules []IgnoreRule
	var decorateRules []DecorateRule
	var addAttributesRules []AddAttributesRule
	for _, pr := range processingRules {
		renameRules = append(renameRules, pr.RenameAttributes...)
		ignoreRules = append(ignoreRules, pr.IgnoreMetrics...)
		addAttributesRules = append(addAttributesRules, pr.AddAttributes...)
		for _, car := range pr.CopyAttributes {
			join := labels.Set{}
			for _, mk := range car.MatchBy {
				join[mk] = struct{}{}
			}
			attrs := labels.Set{}
			for _, mk := range car.Attributes {
				attrs[mk] = struct{}{}
			}
			decorateRules = append(decorateRules, DecorateRule{
				Source:     car.FromMetric,
				Dest:       car.ToMetrics,
				Join:       join,
				Attributes: attrs,
			})
		}
		renameMetricRules = append(renameMetricRules, pr.RenameMetrics...)
	}

	return func(targetMetrics <-chan TargetMetrics) <-chan TargetMetrics {
		processedPairs := make(chan TargetMetrics, queueLength)

		go func() {
			// After finished reading everything from the input target metrics
			// we need to close the result channel to let the emitters know
			// when to stop reading from it.
			defer close(processedPairs)

			for pair := range targetMetrics {
				Filter(&pair, ignoreRules)
				AddAttributes(&pair, addAttributesRules)
				Decorate(&pair, decorateRules)
				Rename(&pair, renameRules)
				RenameMetrics(&pair, renameMetricRules)
				ReNamespaceMetrics(&pair)

				processedPairs <- pair
			}
		}()

		return processedPairs
	}
}
