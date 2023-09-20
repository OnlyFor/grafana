package expr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana/pkg/expr/mathexp"
	"github.com/grafana/grafana/pkg/infra/tracing"
)

type ThresholdCommand struct {
	ReferenceVar string
	RefID        string
	predicate    predicate
}

type thresholdFunc string

type rangeThresholdFunc string

const (
	ThresholdIsAbove        thresholdFunc      = "gt"
	ThresholdIsBelow        thresholdFunc      = "lt"
	ThresholdIsWithinRange  rangeThresholdFunc = "within_range"
	ThresholdIsOutsideRange rangeThresholdFunc = "outside_range"
)

var (
	supportedThresholdFuncs = []string{string(ThresholdIsAbove), string(ThresholdIsBelow), string(ThresholdIsWithinRange), string(ThresholdIsOutsideRange)}
)

func NewThresholdCommand(refID, referenceVar, thresholdFunc string, conditions []float64) (*ThresholdCommand, error) {
	var predicate predicate
	switch strings.ToLower(thresholdFunc) {
	case string(ThresholdIsOutsideRange):
		if len(conditions) < 2 {
			return nil, fmt.Errorf("incorrect number of arguments for threshold function '%s': got %d but need 2", thresholdFunc, len(conditions))
		}
		predicate = outsideRangePredicate{left: conditions[0], right: conditions[1]}
	case string(ThresholdIsWithinRange):
		if len(conditions) < 2 {
			return nil, fmt.Errorf("incorrect number of arguments for threshold function '%s': got %d but need 2", thresholdFunc, len(conditions))
		}
		predicate = withinRangePredicate{left: conditions[0], right: conditions[1]}
	case string(ThresholdIsAbove):
		if len(conditions) < 1 {
			return nil, fmt.Errorf("incorrect number of arguments for threshold function '%s': got %d but need 1", thresholdFunc, len(conditions))
		}
		predicate = greaterThanPredicate{value: conditions[0]}
	case string(ThresholdIsBelow):
		if len(conditions) < 1 {
			return nil, fmt.Errorf("incorrect number of arguments for threshold function '%s': got %d but need 1", thresholdFunc, len(conditions))
		}
		predicate = lessThanPredicate{value: conditions[0]}
	default:
		return nil, fmt.Errorf("expected threshold function to be one of [%s], got %s", strings.Join(supportedThresholdFuncs, ", "), thresholdFunc)
	}

	return &ThresholdCommand{
		RefID:        refID,
		ReferenceVar: referenceVar,
		predicate:    predicate,
	}, nil
}

type ThresholdConditionJSON struct {
	Evaluator ConditionEvalJSON `json:"evaluator"`
}

type ConditionEvalJSON struct {
	Params []float64 `json:"params"`
	Type   string    `json:"type"` // e.g. "gt"
}

// UnmarshalResampleCommand creates a ResampleCMD from Grafana's frontend query.
func UnmarshalThresholdCommand(rn *rawNode) (*ThresholdCommand, error) {
	rawQuery := rn.Query

	rawExpression, ok := rawQuery["expression"]
	if !ok {
		return nil, fmt.Errorf("no variable specified to reference for refId %v", rn.RefID)
	}
	referenceVar, ok := rawExpression.(string)
	if !ok {
		return nil, fmt.Errorf("expected threshold variable to be a string, got %T for refId %v", rawExpression, rn.RefID)
	}

	jsonFromM, err := json.Marshal(rawQuery["conditions"])
	if err != nil {
		return nil, fmt.Errorf("failed to remarshal threshold expression body: %w", err)
	}
	var conditions []ThresholdConditionJSON
	if err = json.Unmarshal(jsonFromM, &conditions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remarshaled threshold expression body: %w", err)
	}

	for _, condition := range conditions {
		if !IsSupportedThresholdFunc(condition.Evaluator.Type) {
			return nil, fmt.Errorf("expected threshold function to be one of %s, got %s", strings.Join(supportedThresholdFuncs, ", "), condition.Evaluator.Type)
		}
	}

	// we only support one condition for now, we might want to turn this in to "OR" expressions later
	if len(conditions) != 1 {
		return nil, fmt.Errorf("threshold expression requires exactly one condition")
	}
	firstCondition := conditions[0]

	return NewThresholdCommand(rn.RefID, referenceVar, firstCondition.Evaluator.Type, firstCondition.Evaluator.Params)
}

// NeedsVars returns the variable names (refIds) that are dependencies
// to execute the command and allows the command to fulfill the Command interface.
func (tc *ThresholdCommand) NeedsVars() []string {
	return []string{tc.ReferenceVar}
}

func (tc *ThresholdCommand) Execute(ctx context.Context, _ time.Time, vars mathexp.Vars, tracer tracing.Tracer) (mathexp.Results, error) {
	_, span := tracer.Start(ctx, "SSE.ExecuteThreshold")
	defer span.End()

	eval := func(maybeValue *float64) *float64 {
		if maybeValue == nil {
			return nil
		}
		var result float64
		if tc.predicate.Eval(*maybeValue) {
			result = 1
		}
		return &result
	}

	refVar := vars[tc.ReferenceVar]
	newRes := mathexp.Results{Values: make(mathexp.Values, 0, len(refVar.Values))}
	for _, val := range refVar.Values {
		switch v := val.(type) {
		case mathexp.Series:
			s := mathexp.NewSeries(tc.RefID, v.GetLabels(), v.Len())
			for i := 0; i < v.Len(); i++ {
				t, value := s.GetPoint(i)
				s.SetPoint(i, t, eval(value))
			}
			newRes.Values = append(newRes.Values, s)
		case mathexp.Number:
			copyV := mathexp.NewNumber(tc.RefID, v.GetLabels())
			copyV.SetValue(eval(v.GetFloat64Value()))
			newRes.Values = append(newRes.Values, copyV)
		case mathexp.Scalar:
			copyV := mathexp.NewScalar(tc.RefID, eval(v.GetFloat64Value()))
			newRes.Values = append(newRes.Values, copyV)
		case mathexp.NoData:
			newRes.Values = append(newRes.Values, mathexp.NewNoData())
		default:
			return newRes, fmt.Errorf("can only reduce type series, got type %v", val.Type())
		}
	}
	return newRes, nil
}

func IsSupportedThresholdFunc(name string) bool {
	isSupported := false

	for _, funcName := range supportedThresholdFuncs {
		if funcName == name {
			isSupported = true
		}
	}

	return isSupported
}

type predicate interface {
	Eval(f float64) bool
	Intersect(p predicate) bool
}

type withinRangePredicate struct {
	left  float64
	right float64
}

func (r withinRangePredicate) Eval(f float64) bool {
	return f > r.left && f < r.right
}

func (r withinRangePredicate) Intersect(p predicate) bool {
	return p.Eval(r.left+1) || p.Eval(r.right-1) || p.Intersect(r)
}

type outsideRangePredicate struct {
	left  float64
	right float64
}

func (r outsideRangePredicate) Eval(f float64) bool {
	return f < r.left || f > r.right
}

func (r outsideRangePredicate) Intersect(p predicate) bool {
	return p.Eval(r.left-1) || p.Eval(r.right+1) || p.Intersect(r)
}

type lessThanPredicate struct {
	value float64
}

func (r lessThanPredicate) Eval(f float64) bool {
	return r.value < f
}

func (r lessThanPredicate) Intersect(p predicate) bool {
	return p.Eval(r.value+1) || p.Intersect(r)
}

type greaterThanPredicate struct {
	value float64
}

func (r greaterThanPredicate) Eval(f float64) bool {
	return r.value > f
}

func (r greaterThanPredicate) Intersect(p predicate) bool {
	return p.Eval(r.value-1) || p.Intersect(r)
}
