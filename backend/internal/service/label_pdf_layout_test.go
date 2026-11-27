package service

import (
	"errors"
	"testing"

	"findus/backend/internal/domain"
)

func TestComputeLabelLayoutMargins(t *testing.T) {
	layout, err := computeLabelLayout(LabelSheetLayout{Cols: 3, Rows: 8, MarginTop: 12, MarginRight: 4, MarginBottom: 9, MarginLeft: 6})
	if err != nil {
		t.Fatal(err)
	}
	if layout.MarginX != 6 || layout.MarginY != 12 {
		t.Fatalf("origin = (%v, %v), want (6, 12)", layout.MarginX, layout.MarginY)
	}
	if wantW, wantH := 200.0/3, 276.0/8; layout.LabelW != wantW || layout.LabelH != wantH {
		t.Fatalf("label size = (%v, %v), want (%v, %v)", layout.LabelW, layout.LabelH, wantW, wantH)
	}
	if layout.LabelsPerPage != 24 {
		t.Fatalf("labels per page = %d, want 24", layout.LabelsPerPage)
	}
}

func TestComputeLabelLayoutRejectsInvalid(t *testing.T) {
	for name, sheet := range map[string]LabelSheetLayout{
		"negative margin":  {Cols: 2, Rows: 7, MarginRight: -1},
		"margins too wide": {Cols: 2, Rows: 7, MarginLeft: 120, MarginRight: 120},
		"too many cols":    {Cols: 11, Rows: 7},
		"too many rows":    {Cols: 2, Rows: 20},
	} {
		if _, err := computeLabelLayout(sheet); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: err = %v, want validation error", name, err)
		}
	}
}
