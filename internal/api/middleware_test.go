package api_test

import (
	"net/http"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/KKittyCatik/music_p2p/internal/metrics"
)

// requestPathLabels returns the set of "path" label values currently present on
// the music_p2p_http_requests_total metric family.
func requestPathLabels(t *testing.T) map[string]bool {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	assert.NoError(t, err)
	paths := map[string]bool{}
	for _, mf := range families {
		if mf.GetName() != "music_p2p_http_requests_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "path" {
					paths[lp.GetValue()] = true
				}
			}
		}
	}
	return paths
}

// TestMetricsMiddlewareUsesRouteTemplate verifies that two requests differing
// only in the high-cardinality {cid} path segment collapse onto a single
// Prometheus series labelled with the route TEMPLATE, rather than spawning one
// new series per concrete CID (which caused unbounded memory growth).
func TestMetricsMiddlewareUsesRouteTemplate(t *testing.T) {
	srv := newTestServer(t)

	const (
		cidA     = "aaaa1111"
		cidB     = "bbbb2222"
		template = "/api/v1/metadata/{cid}"
	)

	before := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues(http.MethodGet, template, "404"))

	// Two requests differing only in the {cid} segment. The empty metadata store
	// returns 404 for both; only the path label is relevant here.
	rrA := doRequest(t, srv, http.MethodGet, "/api/v1/metadata/"+cidA, nil)
	rrB := doRequest(t, srv, http.MethodGet, "/api/v1/metadata/"+cidB, nil)
	assert.Equal(t, http.StatusNotFound, rrA.Code)
	assert.Equal(t, http.StatusNotFound, rrB.Code)

	// Both requests must increment the same templated series (delta == 2),
	// proving they share one label set rather than one series per CID.
	after := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues(http.MethodGet, template, "404"))
	assert.Equal(t, float64(2), after-before)

	// The concrete CID values must never appear as label values.
	paths := requestPathLabels(t)
	assert.True(t, paths[template], "expected templated path label to be present")
	assert.NotContains(t, paths, "/api/v1/metadata/"+cidA)
	assert.NotContains(t, paths, "/api/v1/metadata/"+cidB)
}
