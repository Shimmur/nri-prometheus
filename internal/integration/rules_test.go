// Copyright 2019 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
//nolint:goconst
package integration

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/newrelic/nri-prometheus/internal/pkg/endpoints"
	"github.com/newrelic/nri-prometheus/internal/pkg/labels"
)

func TestConsolideLabels(t *testing.T) {
	t.Skip("Auto-decoration isn't used at this moment.")
	pair := scrapeString(t, prometheusInput)
	AutoDecorateLabels(&pair)
	fmt.Println("PAIR: ", pair.Metrics)
	for _, metric := range pair.Metrics {
		switch metric.name {
		case "redis_exporter_scrapes_total":
			expected := labels.Set{
				"build_date.redis_exporter_build_info":     "2018-07-03-14:18:56",
				"commit_sha.redis_exporter_build_info":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
				"golang_version.redis_exporter_build_info": "go1.9.4",
				"version.redis_exporter_build_info":        "v0.20.2",
			}
			AssertContainsTree(t, metric.attributes, expected)
		case "redis_instantaneous_input_kbps":
			switch metric.attributes["addr"] {
			case "ohai-playground-redis-slave:6379":
				expected := labels.Set{
					"addr":  "ohai-playground-redis-slave:6379",
					"alias": "ohai-playground-redis",
					// Fields added from redis_exporter_build_info
					"build_date.redis_exporter_build_info":     "2018-07-03-14:18:56",
					"commit_sha.redis_exporter_build_info":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
					"golang_version.redis_exporter_build_info": "go1.9.4",
					"version.redis_exporter_build_info":        "v0.20.2",
					// Fields added from the corresponding redis_instance_info entry
					"os.redis_instance_info":             "Linux 4.15.0 x86_64",
					"redis_build_id.redis_instance_info": "c701a4acd98ea64a",
					"redis_mode.redis_instance_info":     "standalone",
					"redis_version.redis_instance_info":  "4.0.10",
					"role.redis_instance_info":           "slave",
				}
				AssertContainsTree(t, metric.attributes, expected)
			case "ohai-playground-redis-master:6379":
				expected := labels.Set{
					"addr":  "ohai-playground-redis-master:6379",
					"alias": "ohai-playground-redis",
					// Fields added from redis_exporter_build_info
					"build_date.redis_exporter_build_info":     "2018-07-03-14:18:56",
					"commit_sha.redis_exporter_build_info":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
					"golang_version.redis_exporter_build_info": "go1.9.4",
					"version.redis_exporter_build_info":        "v0.20.2",
					// Fields added from the corresponding redis_instance_info entry
					"os.redis_instance_info":             "Linux 4.15.0 x86_64",
					"redis_build_id.redis_instance_info": "c701a4acd98ea64a",
					"redis_mode.redis_instance_info":     "standalone",
					"redis_version.redis_instance_info":  "4.0.10",
					"role.redis_instance_info":           "master",
				}
				AssertContainsTree(t, metric.attributes, expected)
			default:
				assert.Failf(t, "unexpected addr field:", "%#v", metric.attributes)
			}
		default:
			assert.True(t, strings.HasSuffix(metric.name, "_info"), "unexpected metric %s", metric.name)
		}
	}
}

func AssertContainsTree(t *testing.T, containing, contained labels.Set) {
	t.Helper()

	for k, v := range contained {
		assert.Contains(t, containing, k)
		assert.Equal(t, containing[k], v)
	}
}

func TestMatchingRules(t *testing.T) {
	entity := scrapeString(t, prometheusInput)
	dc := MatchingDecorate(&entity, []DecorateRule{
		{
			Source: "redis_instance_info",
			Dest:   []string{"redis_exporter_scrapes_total", "redis_instantaneous_input_kbps"},
		},
		{
			Source: "redis_exporter_build_info",
			Dest:   []string{"redis_instance_info"},
		},
	})

	// redis_instance_info links to the two label sets
	assert.Equal(t, "ohai-playground-redis", dc.SourceLabels["redis_instance_info"][0]["alias"])
	assert.Equal(t, "ohai-playground-redis-master:6379", dc.SourceLabels["redis_instance_info"][0]["addr"])
	assert.Equal(t, "ohai-playground-redis", dc.SourceLabels["redis_instance_info"][1]["alias"])
	assert.Equal(t, "ohai-playground-redis-slave:6379", dc.SourceLabels["redis_instance_info"][1]["addr"])

	// redis_exporter_build_info links to its label set
	assert.Equal(t, "2018-07-03-14:18:56", dc.SourceLabels["redis_exporter_build_info"][0]["build_date"])
	assert.Equal(t, "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4", dc.SourceLabels["redis_exporter_build_info"][0]["commit_sha"])
	assert.Equal(t, "go1.9.4", dc.SourceLabels["redis_exporter_build_info"][0]["golang_version"])
	assert.Equal(t, "v0.20.2", dc.SourceLabels["redis_exporter_build_info"][0]["version"])

	// Asserting the destination metrics link to their respective rules
	assert.Len(t, dc.Dests["redis_exporter_scrapes_total"], 1)
	assert.Len(t, dc.Dests["redis_instantaneous_input_kbps"], 1)
	assert.Len(t, dc.Dests["redis_instance_info"], 1)
	assert.Equal(t, "redis_instance_info", dc.Dests["redis_exporter_scrapes_total"][0].Source)
	assert.Equal(t, "redis_instance_info", dc.Dests["redis_instantaneous_input_kbps"][0].Source)
	assert.Equal(t, "redis_exporter_build_info", dc.Dests["redis_instance_info"][0].Source)
}

func TestCopyAttributes(t *testing.T) {
	input := fmt.Sprintf("%s\n%s", prometheusInput,
		`# HELP some_undecorated_stuff
# TYPE some_undecorated_stuff gauge
some_undecorated_stuff{addr="ohai-playground-redis-slave:6379",alias="ohai-playground-redis"} 0
`)

	entity := scrapeString(t, input)

	CopyAttributes(&entity, []DecorateRule{
		{
			Source: "redis_instance_info",
			Dest:   []string{"redis_instantaneous_input_kbps"},
			Join:   labels.Set{"addr": 1},
		},
		{
			Source: "redis_exporter_build_info",
			Dest:   []string{"redis_exporter_scrapes_total", "redis_instantaneous_input_kbps"},
			Join:   labels.Set{},
		},
	})

	for _, metric := range entity.Metrics {
		switch metric.name {
		case "redis_exporter_scrapes_total":
			expected := labels.Set{
				"build_date":     "2018-07-03-14:18:56",
				"commit_sha":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
				"golang_version": "go1.9.4",
				"version":        "v0.20.2",
			}
			AssertContainsTree(t, metric.attributes, expected)
		case "redis_instantaneous_input_kbps":
			switch metric.attributes["addr"] {
			case "ohai-playground-redis-slave:6379":
				expected := labels.Set{
					"addr":  "ohai-playground-redis-slave:6379",
					"alias": "ohai-playground-redis",
					// Fields added from redis_exporter_build_info
					"build_date":     "2018-07-03-14:18:56",
					"commit_sha":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
					"golang_version": "go1.9.4",
					"version":        "v0.20.2",
					// Fields added from the corresponding redis_instance_info entry
					"os":             "Linux 4.15.0 x86_64",
					"redis_build_id": "c701a4acd98ea64a",
					"redis_mode":     "standalone",
					"redis_version":  "4.0.10",
					"role":           "slave",
				}
				AssertContainsTree(t, metric.attributes, expected)
			case "ohai-playground-redis-master:6379":
				expected := labels.Set{
					"addr":  "ohai-playground-redis-master:6379",
					"alias": "ohai-playground-redis",
					// Fields added from redis_exporter_build_info
					"build_date":     "2018-07-03-14:18:56",
					"commit_sha":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
					"golang_version": "go1.9.4",
					"version":        "v0.20.2",
					// Fields added from the corresponding redis_instance_info entry
					"os":             "Linux 4.15.0 x86_64",
					"redis_build_id": "c701a4acd98ea64a",
					"redis_mode":     "standalone",
					"redis_version":  "4.0.10",
					"role":           "master",
				}
				AssertContainsTree(t, metric.attributes, expected)
			default:
				assert.Failf(t, "unexpected addr field:", "%#v", metric.attributes)
			}
		case "some_undecorated_stuff":
			assert.Len(t, metric.attributes, 5)
			assert.Equal(t, "ohai-playground-redis-slave:6379", metric.attributes["addr"])
			assert.Equal(t, "ohai-playground-redis", metric.attributes["alias"])
		default:
			assert.True(t, strings.HasSuffix(metric.name, "_info"), "unexpected metric %s", metric.name)
		}
	}
}

func TestCopyAttributes_withPrefix(t *testing.T) {
	input := fmt.Sprintf("%s\n%s", prometheusInput,
		`# HELP some_undecorated_stuff
# TYPE some_undecorated_stuff gauge
some_undecorated_stuff{addr="ohai-playground-redis-slave:6379",alias="ohai-playground-redis"} 0
`)

	entity := scrapeString(t, input)

	CopyAttributes(&entity, []DecorateRule{
		{
			Source: "redis_instance_info",
			Dest:   []string{"redis_instantaneous_"}, // this is only a prefix
			Join:   labels.Set{"addr": 1},
		},
		{
			Source: "redis_exporter_build_info",
			Dest:   []string{"redis_exporter_scrapes_", "redis_instantaneous_input_kbps"},
			Join:   labels.Set{},
		},
	})

	for _, metric := range entity.Metrics {
		switch metric.name {
		case "redis_exporter_scrapes_total":
			expected := labels.Set{
				"build_date":     "2018-07-03-14:18:56",
				"commit_sha":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
				"golang_version": "go1.9.4",
				"version":        "v0.20.2",
			}
			AssertContainsTree(t, metric.attributes, expected)
		case "redis_instantaneous_input_kbps":
			switch metric.attributes["addr"] {
			case "ohai-playground-redis-slave:6379":
				expected := labels.Set{
					"addr":  "ohai-playground-redis-slave:6379",
					"alias": "ohai-playground-redis",
					// Fields added from redis_exporter_build_info
					"build_date":     "2018-07-03-14:18:56",
					"commit_sha":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
					"golang_version": "go1.9.4",
					"version":        "v0.20.2",
					// Fields added from the corresponding redis_instance_info entry
					"os":             "Linux 4.15.0 x86_64",
					"redis_build_id": "c701a4acd98ea64a",
					"redis_mode":     "standalone",
					"redis_version":  "4.0.10",
					"role":           "slave",
				}
				AssertContainsTree(t, metric.attributes, expected)
			case "ohai-playground-redis-master:6379":
				expected := labels.Set{
					"addr":  "ohai-playground-redis-master:6379",
					"alias": "ohai-playground-redis",
					// Fields added from redis_exporter_build_info
					"build_date":     "2018-07-03-14:18:56",
					"commit_sha":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
					"golang_version": "go1.9.4",
					"version":        "v0.20.2",
					// Fields added from the corresponding redis_instance_info entry
					"os":             "Linux 4.15.0 x86_64",
					"redis_build_id": "c701a4acd98ea64a",
					"redis_mode":     "standalone",
					"redis_version":  "4.0.10",
					"role":           "master",
				}
				AssertContainsTree(t, metric.attributes, expected)
			default:
				assert.Failf(t, "unexpected addr field:", "%#v", metric.attributes)
			}
		case "some_undecorated_stuff":
			assert.Len(t, metric.attributes, 5)
			assert.Equal(t, "ohai-playground-redis-slave:6379", metric.attributes["addr"])
			assert.Equal(t, "ohai-playground-redis", metric.attributes["alias"])
		default:
			assert.True(t, strings.HasSuffix(metric.name, "_info"), "unexpected metric %s", metric.name)
		}
	}
}

func TestCopyAttributes_SelectAttributes(t *testing.T) {
	input := fmt.Sprintf("%s\n%s", prometheusInput,
		`# HELP some_undecorated_stuff
# TYPE some_undecorated_stuff gauge
some_undecorated_stuff{addr="ohai-playground-redis-slave:6379",alias="ohai-playground-redis"} 0
`)

	entity := scrapeString(t, input)

	CopyAttributes(&entity, []DecorateRule{
		{
			Source:     "redis_instance_info",
			Dest:       []string{"redis_instantaneous_"}, // this is only a prefix
			Join:       labels.Set{"addr": 1},
			Attributes: labels.Set{"os": 1, "role": 1},
		},
		{
			Source:     "redis_exporter_build_info",
			Dest:       []string{"redis_exporter_scrapes_", "redis_instantaneous_input_kbps"},
			Join:       labels.Set{},
			Attributes: labels.Set{"version": 1, "golang_version": 1},
		},
	})

	for _, metric := range entity.Metrics {
		switch metric.name {
		case "redis_exporter_scrapes_total":
			expected := labels.Set{
				"golang_version": "go1.9.4",
				"version":        "v0.20.2",
				"cosa":           "fina",
			}
			AssertContainsTree(t, metric.attributes, expected)
			assert.NotContains(t, metric.attributes, "build_date")
			assert.NotContains(t, metric.attributes, "commit_sha")
		case "redis_instantaneous_input_kbps":
			switch metric.attributes["addr"] {
			case "ohai-playground-redis-slave:6379":
				expected := labels.Set{
					"addr":  "ohai-playground-redis-slave:6379",
					"alias": "ohai-playground-redis",
					// Fields added from redis_exporter_build_info
					"golang_version": "go1.9.4",
					"version":        "v0.20.2",
					// Fields added from the corresponding redis_instance_info entry
					"os":   "Linux 4.15.0 x86_64",
					"role": "slave",
				}
				AssertContainsTree(t, metric.attributes, expected)
			case "ohai-playground-redis-master:6379":
				expected := labels.Set{
					"addr":  "ohai-playground-redis-master:6379",
					"alias": "ohai-playground-redis",
					// Fields added from redis_exporter_build_info
					"golang_version": "go1.9.4",
					"version":        "v0.20.2",
					// Fields added from the corresponding redis_instance_info entry
					"os":   "Linux 4.15.0 x86_64",
					"role": "master",
				}
				AssertContainsTree(t, metric.attributes, expected)
			default:
				assert.Failf(t, "unexpected addr field:", "%#v", metric.attributes)
			}
			assert.NotContains(t, metric.attributes, "build_date")
			assert.NotContains(t, metric.attributes, "commit_sha")
			assert.NotContains(t, metric.attributes, "redis_build_id")
			assert.NotContains(t, metric.attributes, "redis_mode")
			assert.NotContains(t, metric.attributes, "redis_version")
		case "some_undecorated_stuff":
			assert.Len(t, metric.attributes, 5)
			assert.Equal(t, "ohai-playground-redis-slave:6379", metric.attributes["addr"])
			assert.Equal(t, "ohai-playground-redis", metric.attributes["alias"])
		default:
			assert.True(t, strings.HasSuffix(metric.name, "_info"), "unexpected metric %s", metric.name)
		}
	}
}

func TestDecorate(t *testing.T) {
	targetURL, _ := url.Parse("https://user:password@newrelic.com")
	se := []TargetMetrics{{
		Target: endpoints.Target{
			Name: "a_simple_target",
			URL:  *targetURL,
			Object: endpoints.Object{
				Labels: labels.Set{
					"hello": "friend",
					"bye":   "boy",
				},
			},
		},
		Metrics: []Metric{
			{name: "metric1", value: 3, attributes: labels.Set{"md1": "v1", "md2": "v2", "attr1": "val1"}},
			{name: "metric2", value: 3, attributes: labels.Set{"md3": "v3", "md4": "v4", "attr2": "val2"}},
		},
	}}

	Decorate(&se[0], []DecorateRule{})

	assert.Equal(t, se[0].Metrics[0].attributes, labels.Set{"hello": "friend", "bye": "boy", "md1": "v1", "md2": "v2", "attr1": "val1", "scrapedTargetURL": "https://user:xxxxx@newrelic.com"})
	assert.Equal(t, se[0].Metrics[1].attributes, labels.Set{"hello": "friend", "bye": "boy", "md3": "v3", "md4": "v4", "attr2": "val2", "scrapedTargetURL": "https://user:xxxxx@newrelic.com"})

}

func TestRenameRules(t *testing.T) {
	entity := scrapeString(t, prometheusInput)

	rules := []RenameRule{
		{
			MetricPrefix: "redis_exporter",
			Attributes: map[string]interface{}{
				"build_date": "build_on",
			},
		},
		{
			MetricPrefix: "redis_instantaneous_",
			Attributes: map[string]interface{}{
				"alias": "also_named_as",
				"addr":  "address",
			},
		},
	}

	Rename(&entity, rules)

	for _, metric := range entity.Metrics {
		switch metric.name {
		case "redis_exporter_build_info":
			expected := labels.Set{
				"build_on":       "2018-07-03-14:18:56",
				"build_date":     "2018-07-03-14:18:56",
				"commit_sha":     "3e15af27aac37e114b32a07f5e9dc0510f4cbfc4",
				"golang_version": "go1.9.4",
				"version":        "v0.20.2",
			}
			AssertContainsTree(t, metric.attributes, expected)
		case "redis_instantaneous_input_kbps":
			switch metric.attributes["address"] {
			case "ohai-playground-redis-slave:6379":
				expected := labels.Set{
					"address":       "ohai-playground-redis-slave:6379",
					"addr":          "ohai-playground-redis-slave:6379",
					"also_named_as": "ohai-playground-redis",
					"alias":         "ohai-playground-redis",
				}
				AssertContainsTree(t, metric.attributes, expected)
			case "ohai-playground-redis-master:6379":
				expected := labels.Set{
					"address":       "ohai-playground-redis-master:6379",
					"addr":          "ohai-playground-redis-master:6379",
					"also_named_as": "ohai-playground-redis",
					"alias":         "ohai-playground-redis",
				}
				AssertContainsTree(t, metric.attributes, expected)
			default:
				assert.Failf(t, "unexpected address field:", "%#v", metric.attributes)
			}
		}
	}
}

func TestAddAttributesRules(t *testing.T) {
	entity := scrapeString(t, prometheusInput)
	AddAttributes(&entity, []AddAttributesRule{
		{
			MetricPrefix: "",
			Attributes: map[string]interface{}{
				"new-attribute": "new-value",
			},
		},
		{
			MetricPrefix: "redis_exporter_",
			Attributes: map[string]interface{}{
				"another-new-attribute": "new-value",
			},
		},
	})
	for _, metric := range entity.Metrics {
		switch metric.name {
		case "redis_exporter_build_info":
			assert.Contains(t, metric.attributes, "another-new-attribute")
			assert.Contains(t, metric.attributes, "new-attribute")
		case "redis_exporter_scrapes_total":
			assert.Contains(t, metric.attributes, "another-new-attribute")
			assert.Contains(t, metric.attributes, "new-attribute")
		default:
			assert.NotContains(t, metric.attributes, "another-new-attribute")
			assert.Contains(t, metric.attributes, "new-attribute")
		}
	}
}

func TestIgnoreRules(t *testing.T) {
	entity := scrapeString(t, prometheusInput)
	Filter(&entity, []IgnoreRule{
		{
			Prefixes: []string{"redis_exporter_scrapes"},
		},
		{
			Prefixes: []string{"redis_instance"},
		},
	})

	actual := map[string]interface{}{}
	for _, metric := range entity.Metrics {
		switch metric.name {
		case "redis_exporter_build_info":
			actual[metric.name] = 1
		case "redis_instantaneous_input_kbps":
			actual[metric.name] = 1
		case "redis_exporter_scrapes_total":
			require.Fail(t, "redis_exporter_scrapes_total must have been filtered")
		case "redis_instance_info":
			require.Fail(t, "redis_instance_info must have been filtered")
		default:
			require.Fail(t, "unexpected metric", "%#v", metric)
		}
	}
	assert.Contains(t, actual, "redis_exporter_build_info")
	assert.Contains(t, actual, "redis_instantaneous_input_kbps")
}

func TestIgnoreRules_PrefixesWithExceptions(t *testing.T) {
	entity := scrapeString(t, prometheusInput)
	Filter(&entity, []IgnoreRule{
		{
			Prefixes: []string{"redis_exporter_scrapes"},
		},
		{
			Prefixes: []string{"redis_instan"}, Except: []string{"redis_instance"},
		},
	})

	actual := map[string]interface{}{}
	for _, metric := range entity.Metrics {
		switch metric.name {
		case "redis_exporter_build_info":
			actual[metric.name] = 1
		case "redis_instantaneous_input_kbps":
			require.Fail(t, "redis_instantaneous_input_kbps must have been filtered")
		case "redis_exporter_scrapes_total":
			require.Fail(t, "redis_exporter_scrapes_total must have been filtered")
		case "redis_instance_info":
			actual[metric.name] = 1
		default:
			require.Fail(t, "unexpected metric", "%#v", metric)
		}
	}

	assert.Len(t, actual, 2)
	assert.Contains(t, actual, "redis_exporter_build_info")
	assert.Contains(t, actual, "redis_instance_info")
}

func TestIgnoreRules_IgnoreAllExceptExceptions(t *testing.T) {
	entity := scrapeString(t, prometheusInput)
	Filter(&entity, []IgnoreRule{
		{
			Except: []string{"redis_exporter_build"},
		},
		{
			Except: []string{"redis_instance"},
		},
	})

	actual := map[string]interface{}{}
	for _, metric := range entity.Metrics {
		switch metric.name {
		case "redis_exporter_build_info":
			actual[metric.name] = 1
		case "redis_instantaneous_input_kbps":
			require.Fail(t, "redis_instantaneous_input_kbps must have been filtered")
		case "redis_exporter_scrapes_total":
			require.Fail(t, "redis_exporter_scrapes_total must have been filtered")
		case "redis_instance_info":
			actual[metric.name] = 1
		default:
			require.Fail(t, "unexpected metric", "%#v", metric)
		}
	}

	assert.Len(t, actual, 2)
	assert.Contains(t, actual, "redis_exporter_build_info")
	assert.Contains(t, actual, "redis_instance_info")
}

func TestRenameMetrics(t *testing.T) {
	entity := scrapeString(t, prometheusInput)
	RenameMetrics(&entity, []RenameMetricRule{
		{
			FromMetric: "redis_exporter_build_info",
			ToMetric:   "heorot",
		},
	})

	var found bool

	for _, metric := range entity.Metrics {
		assert.NotEqual(t, metric.name, "redis_exporter_build_info")
		if metric.name == "heorot" {
			found = true
		}
	}

	assert.True(t, found)
}

func TestRenamespaceMetrics(t *testing.T) {
	entity := scrapeString(t, prometheusInput)
	entity.Target.MetricNamespace = "beowulf"
	ReNamespaceMetrics(&entity)

	for _, metric := range entity.Metrics {
		assert.Regexp(t, regexp.MustCompile(`^beowulf\.`), metric.name)
	}
}
