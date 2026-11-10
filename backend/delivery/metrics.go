package delivery

import "log/slog"

// certharbor-control has no existing Prometheus endpoint or metrics client
// in use today: github.com/prometheus/client_golang appears only as an
// indirect dependency pulled in transitively by another module, nothing in
// backend/ constructs a registry or exposes /metrics. Rather than stand up
// a new Prometheus stack unasked (explicitly out of scope per the brief),
// Section 10's two metrics are emitted as structured slog lines with a
// stable "metric" key, value and labels, which is the simplest thing
// consistent with Section 10's intent and is easy to scrape with any
// log-based metrics pipeline (e.g. promtail/Loki) later without a code
// change if/when certharbor adopts one.

// daysRemainingMetric logs the days-remaining gauge for one binding.
func daysRemainingMetric(bindingID, certificateName string, daysRemaining float64) {
	slog.Info("delivery_metric",
		"metric", "delivery_days_remaining",
		"binding_id", bindingID,
		"certificate_name", certificateName,
		"value", daysRemaining,
	)
}

// pushResultMetric logs the push-attempt counter for one binding/result.
func pushResultMetric(bindingID, targetType, result string) {
	slog.Info("delivery_metric",
		"metric", "delivery_push_attempts_total",
		"binding_id", bindingID,
		"target_type", targetType,
		"result", result,
		"value", 1,
	)
}

// consecutiveFailuresMetric logs the consecutive_failures gauge for one
// binding, and flags whether it has crossed alertAfterFailures.
func consecutiveFailuresMetric(bindingID string, consecutiveFailures, alertAfterFailures int) {
	slog.Info("delivery_metric",
		"metric", "delivery_consecutive_failures",
		"binding_id", bindingID,
		"value", consecutiveFailures,
		"alert", consecutiveFailures >= alertAfterFailures,
	)
}
