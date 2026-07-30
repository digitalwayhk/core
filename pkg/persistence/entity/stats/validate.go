package stats

import (
	"fmt"
	"strings"
)

// Validate 校验 StatSpec 可注册。
func Validate(s StatSpec) error {
	if strings.TrimSpace(s.Code) == "" {
		return fmt.Errorf("code 不能为空")
	}
	if s.Fact == nil {
		return fmt.Errorf("fact 不能为空")
	}
	switch s.Grain {
	case GrainYear, GrainQuarter, GrainMonth, GrainWeek, GrainDay:
	default:
		return fmt.Errorf("不支持的 grain: %q", s.Grain)
	}
	if len(s.Metrics) == 0 {
		return fmt.Errorf("至少声明一个 metric")
	}

	factFields, err := collectFields(s.Fact)
	if err != nil {
		return fmt.Errorf("fact: %w", err)
	}
	timeField := s.TimeField
	if timeField == "" {
		timeField = "CreatedAt"
	}
	tf, err := requireField(factFields, timeField)
	if err != nil {
		return fmt.Errorf("timeField: %w", err)
	}
	if !tf.IsTime {
		return fmt.Errorf("timeField %s 必须是 time.Time", timeField)
	}

	for i, m := range s.Metrics {
		switch m.Kind {
		case MetricCount:
			// ok
		case MetricSum, MetricAvg:
			if strings.TrimSpace(m.Field) == "" {
				return fmt.Errorf("metrics[%d] %s 需要 Field", i, m.Kind)
			}
			fm, err := requireField(factFields, m.Field)
			if err != nil {
				return fmt.Errorf("metrics[%d]: %w", i, err)
			}
			if !fm.IsNumber {
				return fmt.Errorf("metrics[%d] 字段 %s 不是数值类型", i, m.Field)
			}
		default:
			return fmt.Errorf("metrics[%d] 不支持的 kind: %q", i, m.Kind)
		}
	}

	for i, d := range s.Dimensions {
		if strings.TrimSpace(d.Field) == "" {
			return fmt.Errorf("dimensions[%d].Field 不能为空", i)
		}
		if _, err := requireField(factFields, d.Field); err != nil {
			return fmt.Errorf("dimensions[%d]: %w", i, err)
		}
		for _, ff := range d.DisplayFromFact {
			if _, err := requireField(factFields, ff); err != nil {
				return fmt.Errorf("dimensions[%d].DisplayFromFact: %w", i, err)
			}
		}
		if d.BaseModel != nil && !d.NoDisplay {
			baseFields, err := collectFields(d.BaseModel)
			if err != nil {
				return fmt.Errorf("dimensions[%d].BaseModel: %w", i, err)
			}
			for _, df := range d.ResolvedDisplayFields() {
				if _, err := requireField(baseFields, df); err != nil {
					return fmt.Errorf("dimensions[%d].DisplayFields: %w（可显式设置 DisplayFields）", i, err)
				}
			}
		}
	}
	return nil
}
