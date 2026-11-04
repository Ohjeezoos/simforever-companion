package store

import "encoding/json"

type NewMeasurement struct {
	GameVersionID int32
	BuildID       *int32
	SpellID       int32
	Kind          string
	Value         float64
	CILow         *float64
	CIHigh        *float64
	N             int32
	Method        string
	Sources       json.RawMessage
	Verified      bool
	Notes         string
}
