// Package rules generates Prometheus recording and alert rules YAML from the
// same service configuration that drives dashboard generation.
package rules

// PrometheusRule is a Kubernetes PrometheusRule CR that wraps rule groups
// for deployment via the Prometheus Operator.
type PrometheusRule struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   PrometheusRuleMetadata `yaml:"metadata"`
	Spec       PrometheusRuleSpec     `yaml:"spec"`
}

// PrometheusRuleMetadata holds the Kubernetes object metadata for a PrometheusRule.
type PrometheusRuleMetadata struct {
	Name      string            `yaml:"name"`
	Namespace string            `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

// PrometheusRuleSpec holds the spec for a PrometheusRule CR.
type PrometheusRuleSpec struct {
	Groups []RuleGroup `yaml:"groups"`
}

// RuleFile is the top-level Prometheus rules file structure.
type RuleFile struct {
	Groups []RuleGroup `yaml:"groups"`
}

// RuleGroup is a named set of recording or alert rules.
type RuleGroup struct {
	Name     string `yaml:"name"`
	Interval string `yaml:"interval,omitempty"`
	Rules    []Rule `yaml:"rules"`
}

// Rule represents a single recording or alert rule.
type Rule struct {
	// Recording rule fields.
	Record string `yaml:"record,omitempty"`

	// Alert rule fields.
	Alert string `yaml:"alert,omitempty"`
	For   string `yaml:"for,omitempty"`

	// Common fields.
	Expr        string            `yaml:"expr"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

// ServiceConfig mirrors the main config's service definition for rules generation.
type ServiceConfig struct {
	Key         string
	Label       string
	ShareMetric string
}
