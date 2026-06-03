// Package automation is the event→conditions→actions half of the workflow
// system (Pack 2 §3). Domain code (or a scheduler) emits a named event with a
// payload; the dispatcher loads active automation_rules for that event,
// evaluates their conditions against the payload, and enqueues matching rules'
// actions as river jobs — recording an automation_executions row per run so a
// failed rule is never silently dropped.
package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"b2bcommerce/internal/store/gen"
)

// Enqueuer schedules an automation action to run as a background job. Satisfied
// by *queue.Enqueuer.
type Enqueuer interface {
	EnqueueAutomationAction(ctx context.Context, key string, params, payload map[string]any) error
}

// condition is one rule condition: payload[field] <op> value.
type condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

// actionSpec is one configured action: {"key": "...", "params": {...}}.
type actionSpec struct {
	Key    string         `json:"key"`
	Params map[string]any `json:"params"`
}

// Dispatcher resolves and fans out automation rules for emitted events.
type Dispatcher struct {
	pool *pgxpool.Pool
	enq  Enqueuer
}

func NewDispatcher(pool *pgxpool.Pool, enq Enqueuer) *Dispatcher {
	return &Dispatcher{pool: pool, enq: enq}
}

// Emit dispatches an event: every active rule for the event whose conditions
// match the payload has its actions enqueued, and gets an execution row.
func (d *Dispatcher) Emit(ctx context.Context, event string, payload map[string]any) error {
	q := gen.New(d.pool)
	rules, err := q.ListAutomationRulesByEvent(ctx, event)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if !matchAll(parseConditions(rule.Conditions), payload) {
			continue
		}
		var runErr error
		for _, a := range parseActions(rule.Actions) {
			if e := d.enq.EnqueueAutomationAction(ctx, a.Key, a.Params, payload); e != nil {
				runErr = e
			}
		}
		status, result := "ok", []byte(`{}`)
		if runErr != nil {
			status = "error"
			result, _ = json.Marshal(map[string]any{"error": runErr.Error()})
		}
		pj, _ := json.Marshal(payload)
		_ = q.RecordAutomationExecution(ctx, gen.RecordAutomationExecutionParams{
			RuleID: rule.ID, EventPayload: pj, Status: status, Result: result,
		})
	}
	return nil
}

func parseConditions(raw []byte) []condition {
	if len(raw) == 0 {
		return nil
	}
	var out []condition
	_ = json.Unmarshal(raw, &out)
	return out
}

func parseActions(raw []byte) []actionSpec {
	if len(raw) == 0 {
		return nil
	}
	var out []actionSpec
	_ = json.Unmarshal(raw, &out)
	return out
}

// matchAll reports whether every condition holds against the payload (an empty
// condition set always matches).
func matchAll(conds []condition, payload map[string]any) bool {
	for _, c := range conds {
		if !match(c, payload[c.Field]) {
			return false
		}
	}
	return true
}

func match(c condition, actual any) bool {
	switch c.Op {
	case "eq", "":
		return fmt.Sprint(actual) == fmt.Sprint(c.Value)
	case "ne":
		return fmt.Sprint(actual) != fmt.Sprint(c.Value)
	case "gt", "gte", "lt", "lte":
		af, aok := toFloat(actual)
		bf, bok := toFloat(c.Value)
		if !aok || !bok {
			return false
		}
		switch c.Op {
		case "gt":
			return af > bf
		case "gte":
			return af >= bf
		case "lt":
			return af < bf
		default:
			return af <= bf
		}
	default:
		return false
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}
