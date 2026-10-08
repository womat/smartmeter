package meters

import (
	"math"
	"strings"
	"testing"
)

func TestEvalExpression(t *testing.T) {
	vars := map[string]float64{"power_l1": 100, "power_l2": -40, "power_l3": 10, "voltage_l1": 230}

	for _, tc := range []struct {
		expr string
		want float64
	}{
		{"{power_l1} + {power_l2} + {power_l3}", 70},
		{"power_l1 + power_l2", 60}, // braces are optional
		{"${power_l1} * 2", 200},    // ${} is accepted as well
		{"2 + 3 * 4", 14},           // * before +
		{"(2 + 3) * 4", 20},         // parentheses
		{"10 - 4 - 3", 3},           // left-associative
		{"12 / 4 / 3", 1},           // left-associative
		{"-{power_l2}", 40},         // unary minus
		{"+{power_l3}", 10},         // unary plus
		{"ABS({power_l2})", 40},     // ABS
		{"SQRT(16) + sqrt(9)", 7},   // SQRT, case-insensitive
		{"{voltage_l1} * 1.732050807568877", 230 * 1.732050807568877},
		{"0.5 * {power_l1}", 50},
	} {
		got, err := evalExpression(tc.expr, vars)
		if err != nil || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("evalExpression(%q) = %v, %v; want %v", tc.expr, got, err, tc.want)
		}
	}
}

func TestEvalExpressionErrors(t *testing.T) {
	vars := map[string]float64{"power_l1": 100, "zero": 0}

	for expr, want := range map[string]string{
		"{power_x}":         "unknown field power_x",
		"{power_l1} / zero": "division by zero",
		"SQRT(-1)":          "sqrt of negative value",
		"LOG(10)":           "unsupported function LOG",
		"(1 + 2":            "missing closing parenthesis",
		"ABS(1":             "missing closing parenthesis for ABS",
		"1 +":               "expected number",
		"1 2":               "unexpected token",
	} {
		if _, err := evalExpression(expr, vars); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("evalExpression(%q) error = %v, want %q", expr, err, want)
		}
	}
}
