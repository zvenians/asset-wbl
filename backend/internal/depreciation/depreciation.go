package depreciation

import "errors"

// All amounts are integer cents. The caller stores them in numeric(18,2).
type Method string

const (
	StraightLine    Method = "straight_line"
	Declining       Method = "declining"
	DoubleDeclining Method = "double_declining"
	None            Method = "none"
)

type Input struct {
	Cost, Residual, Accumulated int64
	LifeMonths, ElapsedMonths   int
	Method                      Method
}
type Result struct{ Charge, Accumulated, BookValue int64 }

func Calculate(v Input) (Result, error) {
	if v.Cost < 0 || v.Residual < 0 || v.Residual > v.Cost || v.Accumulated < 0 || v.Accumulated > v.Cost-v.Residual {
		return Result{}, errors.New("invalid depreciation basis")
	}
	if v.Method != None && v.LifeMonths <= 0 {
		return Result{}, errors.New("life months must be positive")
	}
	if v.ElapsedMonths < 0 {
		return Result{}, errors.New("elapsed months cannot be negative")
	}
	remaining := v.Cost - v.Residual - v.Accumulated
	if remaining < 0 {
		return Result{}, errors.New("negative remaining basis")
	}
	var charge int64
	switch v.Method {
	case None:
		charge = 0
	case StraightLine:
		if v.ElapsedMonths < v.LifeMonths {
			charge = (v.Cost - v.Residual + int64(v.LifeMonths/2)) / int64(v.LifeMonths)
		}
	case Declining:
		if v.ElapsedMonths < v.LifeMonths {
			charge = (v.Cost - v.Accumulated - v.Residual) / int64(v.LifeMonths-v.ElapsedMonths)
		}
	case DoubleDeclining:
		if v.ElapsedMonths < v.LifeMonths {
			charge = (v.Cost - v.Accumulated) * 2 / int64(v.LifeMonths)
		}
	default:
		return Result{}, errors.New("unknown method")
	}
	if v.ElapsedMonths >= v.LifeMonths && v.Method != None {
		charge = remaining
	}
	if charge > remaining {
		charge = remaining
	}
	if charge < 0 {
		charge = 0
	}
	return Result{Charge: charge, Accumulated: v.Accumulated + charge, BookValue: v.Cost - v.Accumulated - charge}, nil
}
