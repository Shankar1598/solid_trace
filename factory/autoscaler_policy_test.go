package factory

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// The Autoscaler (0.6.0) loads a policy file when its blocks decode, and checks the rest only
// at the first evaluation, which needs Nomad and Google Cloud. policyProblems checks that rest
// from the Autoscaler's source: these are its names.
var (
	// The strategies in the Autoscaler's binary.
	autoscalerStrategies = []string{"target-value", "pass-through", "threshold", "fixed-value"}
	// The cluster queries of the nomad-apm plugin.
	nomadAPMQueries = []string{"percentage-allocated_cpu", "percentage-allocated_memory"}
	// The node selector strategies of a cluster target.
	nodeSelectors = []string{"least_busy", "newest_create_index", "empty", "empty_ignore_system"}
	// The keys of the gce-mig target's config. The target ignores another key, so a key with a
	// typo takes the default (node_selector_strategy: least_busy, which can remove a busy VM).
	gceMIGKeys = []string{"project", "region", "zone", "mig_name", "credentials", "retry_attempts",
		"node_class", "datacenter", "node_pool", "node_drain_deadline", "node_drain_ignore_system_jobs",
		"node_purge", "node_selector_strategy", "dry-run"}
)

// policyProblems is what is wrong with the policy file text, with the agent's config: the
// APMs and the targets that the config names (and the nomad-apm, which loads with no block).
func policyProblems(config, name, text string) []string {
	var problems []string
	bad := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	agent, err := parseHCL("agent.hcl", config)
	if err != nil {
		return []string{err.Error()}
	}
	apms, targets := []string{"nomad-apm"}, []string{}
	for _, b := range agent.Blocks {
		switch {
		case b.Type == "apm" && len(b.Labels) == 1:
			apms = append(apms, b.Labels[0])
		case b.Type == "target" && len(b.Labels) == 1:
			targets = append(targets, b.Labels[0])
		}
	}

	file, err := parseHCL(name, text)
	if err != nil {
		return []string{err.Error()}
	}
	scaling := blocks(file, "scaling")
	if len(scaling) != 1 {
		return []string{fmt.Sprintf("%s has %d scaling blocks, want 1", name, len(scaling))}
	}
	if kind := stringAttr(scaling[0].Body, "type"); kind != "cluster" {
		bad("the scaling type is %q, want cluster", kind)
	}
	policy := blocks(scaling[0].Body, "policy")
	if len(policy) != 1 {
		return append(problems, fmt.Sprintf("the scaling block has %d policy blocks, want 1", len(policy)))
	}
	checks := blocks(policy[0].Body, "check")
	if len(checks) == 0 {
		bad("the policy has no check")
	}
	for _, c := range checks {
		source, query := stringAttr(c.Body, "source"), stringAttr(c.Body, "query")
		if !slices.Contains(apms, source) {
			bad("the check %v reads the APM %q, which the agent does not have (%v)", c.Labels, source, apms)
		}
		if source == "nomad-apm" && !slices.Contains(nomadAPMQueries, query) {
			bad("the check %v has the nomad-apm query %q, not one of %v", c.Labels, query, nomadAPMQueries)
		}
		if query == "" {
			bad("the check %v has no query", c.Labels)
		}
		strategies := blocks(c.Body, "strategy")
		if len(strategies) != 1 || len(strategies[0].Labels) != 1 {
			bad("the check %v has %d strategies, want 1", c.Labels, len(strategies))
			continue
		}
		strategy := strategies[0]
		if !slices.Contains(autoscalerStrategies, strategy.Labels[0]) {
			bad("the check %v has the strategy %q, not one of %v", c.Labels, strategy.Labels[0], autoscalerStrategies)
		}
		if strategy.Labels[0] == "target-value" {
			if v, ok := attrValue(strategy.Body, "target"); !ok || v.Type() != cty.Number || v.LessThanOrEqualTo(cty.Zero).True() {
				bad("the target-value strategy of the check %v has no target over 0", c.Labels)
			}
		}
	}
	target := blocks(policy[0].Body, "target")
	if len(target) != 1 || len(target[0].Labels) != 1 {
		return append(problems, fmt.Sprintf("the policy has %d targets, want 1", len(target)))
	}
	if !slices.Contains(targets, target[0].Labels[0]) {
		bad("the target %q is not one that the agent has (%v)", target[0].Labels[0], targets)
	}
	if target[0].Labels[0] != "gce-mig" {
		return problems
	}
	body := target[0].Body
	for key := range body.Attributes {
		if !slices.Contains(gceMIGKeys, key) {
			bad("the gce-mig target has the key %q, which it does not read", key)
		}
	}
	for _, key := range []string{"project", "mig_name"} {
		if stringAttr(body, key) == "" {
			bad("the gce-mig target has no %s", key)
		}
	}
	if (stringAttr(body, "zone") == "") == (stringAttr(body, "region") == "") {
		bad("the gce-mig target needs a zone or a region, and not both")
	}
	if stringAttr(body, "node_pool") == "" && stringAttr(body, "node_class") == "" && stringAttr(body, "datacenter") == "" {
		bad("the gce-mig target has no node_pool, node_class or datacenter: the nomad-apm cannot find its nodes")
	}
	if s := stringAttr(body, "node_selector_strategy"); s != "" && !slices.Contains(nodeSelectors, s) {
		bad("the node_selector_strategy %q is not one of %v", s, nodeSelectors)
	}
	return problems
}

func parseHCL(name, text string) (*hclsyntax.Body, error) {
	file, diags := hclsyntax.ParseConfig([]byte(text), name, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, fmt.Errorf("%s does not parse: %s", name, diags.Error())
	}
	return file.Body.(*hclsyntax.Body), nil
}

func blocks(body *hclsyntax.Body, kind string) []*hclsyntax.Block {
	var found []*hclsyntax.Block
	for _, b := range body.Blocks {
		if b.Type == kind {
			found = append(found, b)
		}
	}
	return found
}

// attrValue is the value of a constant attribute.
func attrValue(body *hclsyntax.Body, name string) (cty.Value, bool) {
	a, ok := body.Attributes[name]
	if !ok {
		return cty.NilVal, false
	}
	v, diags := a.Expr.Value(nil)
	return v, !diags.HasErrors() && v.IsKnown() && !v.IsNull()
}

// stringAttr is the value of a constant string attribute ("" is none).
func stringAttr(body *hclsyntax.Body, name string) string {
	if v, ok := attrValue(body, name); ok && v.Type() == cty.String {
		return v.AsString()
	}
	return ""
}

// A check of the static check: each change makes a policy that the Autoscaler loads, but that
// fails at its first evaluation, or that does not do what docs/factory.md says.
func TestTheStaticPolicyCheckFindsWhatTheAutoscalerChecksOnlyAtItsFirstEvaluation(t *testing.T) {
	config := reference(t, "autoscaler", "autoscaler.hcl")
	standard := reference(t, "autoscaler", "policies", "standard.hcl")
	if problems := policyProblems(config, "standard.hcl", standard); len(problems) != 0 {
		t.Fatalf("the reference policy has problems: %q", problems)
	}
	for name, change := range map[string][2]string{
		"a strategy that is not the Autoscaler's": {`strategy "target-value"`, `strategy "target-valu"`},
		"target-value with no target":             {"target = 70", "goal = 70"},
		"an APM that the agent does not have":     {`source = "nomad-apm"`, `source = "prometheus"`},
		"a nomad-apm query that is not one":       {`"percentage-allocated_memory"`, `"percentage-allocated_mem"`},
		"a target that the agent does not have":   {`target "gce-mig"`, `target "aws-asg"`},
		"a node selector that is not one":         {`node_selector_strategy = "empty"`, `node_selector_strategy = "emtpy"`},
		"a node selector key with a typo":         {`node_selector_strategy =`, `node_selector_stratgy =`},
		"no managed instance group":               {`mig_name = "factory-agents-standard"`, ``},
		"no zone and no region":                   {`zone     = "europe-west1-b"`, ``},
		"no node pool, class or datacenter":       {`node_pool              = "standard"`, ``},
		"a scaling type that is not cluster":      {`type    = "cluster"`, `type    = "horizontal"`},
		"a syntax error":                          {`policy {`, `policy {{`},
	} {
		bad := strings.Replace(standard, change[0], change[1], 1)
		if bad == standard {
			t.Fatalf("%s: the reference policy has no %q", name, change[0])
		}
		if problems := policyProblems(config, "bad.hcl", bad); len(problems) == 0 {
			t.Errorf("%s: the static check finds no problem", name)
		}
	}
}
