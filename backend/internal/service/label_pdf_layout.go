package service

import (
	"bytes"
	"fmt"
	"math"

	"findus/backend/internal/domain"

	"github.com/jung-kurt/gofpdf"
)

const (
	labelPageW     = 210.0
	labelPageH     = 297.0
	labelMinW      = 20.0 // smaller cells would make QR, text and lines overflow
	labelMinH      = 15.0
	labelLeftRatio = 0.46
	ptToMM         = 0.352778 // 1 point in mm
)

// LabelSheetLayout describes the sticker grid on an A4 page; margins are in mm.
type LabelSheetLayout struct {
	Cols         int     `json:"cols"` // 0 = default (2)
	Rows         int     `json:"rows"` // 0 = auto-fit
	MarginTop    float64 `json:"margin_top"`
	MarginRight  float64 `json:"margin_right"`
	MarginBottom float64 `json:"margin_bottom"`
	MarginLeft   float64 `json:"margin_left"`
	HideBorder   bool    `json:"hide_border"`
}

type labelContent struct {
	QRPath      string
	PrimaryText string
}

type labelLayout struct {
	HideBorder    bool
	Cols          int
	Rows          int
	MarginX       float64
	MarginY       float64
	LabelW        float64
	LabelH        float64
	LabelsPerPage int
	LeftW         float64
	QRSize        float64
	QRTopPad      float64
	TextGap       float64 // gap between QR bottom and text
	TextCellH     float64 // height of the text cell
	FontSize      float64
	NumLines      int
	LineSpacing   float64
}

func computeLabelLayout(sheet LabelSheetLayout) (labelLayout, error) {
	if min(sheet.MarginTop, sheet.MarginRight, sheet.MarginBottom, sheet.MarginLeft) < 0 {
		return labelLayout{}, fmt.Errorf("%w: margins must not be negative", domain.ErrValidation)
	}
	cols, rows := sheet.Cols, sheet.Rows
	usableW := labelPageW - sheet.MarginLeft - sheet.MarginRight
	usableH := labelPageH - sheet.MarginTop - sheet.MarginBottom

	if cols < 1 {
		cols = 2
	}
	labelW := usableW / float64(cols)

	if rows < 1 {
		rows = int(usableH / 38.0)
		if rows < 1 {
			rows = 1
		}
	}
	labelH := usableH / float64(rows)

	if labelW < labelMinW || labelH < labelMinH {
		return labelLayout{}, fmt.Errorf("%w: labels too small (min %.0f x %.0f mm), reduce margins, columns or rows", domain.ErrValidation, labelMinW, labelMinH)
	}

	leftW := labelW * labelLeftRatio

	// Font size scales with label width
	fontSize := 10.0
	if labelW < 70 {
		fontSize = 8.0
	}
	if labelW < 50 {
		fontSize = 7.0
	}

	const (
		textGap  = 1.5 // gap between QR bottom and text
		cellPadH = 6.0 // total vertical padding inside the left cell (top + bottom)
		cellPadW = 6.0 // horizontal padding inside the left cell (left + right)
	)

	textCellH := fontSize*ptToMM + 2.5 // font height in mm + internal padding

	// QR size: must leave room for text+gap and vertical padding
	maxQRFromH := labelH - textCellH - textGap - cellPadH
	maxQRFromW := leftW - cellPadW
	qrSize := math.Min(maxQRFromH, maxQRFromW)
	if qrSize < 5 {
		qrSize = 5
	}

	// Center the QR+text block vertically in the left cell
	blockH := qrSize + textGap + textCellH
	qrTopPad := (labelH - blockH) / 2
	if qrTopPad < 2 {
		qrTopPad = 2
	}

	// Lines on the right: preferred spacing, count scales with label height
	lineSpacing := 8.0
	if labelH < 25 {
		lineSpacing = 6.0
	}
	numLines := int((labelH - 10) / lineSpacing)
	if numLines > 6 {
		numLines = 6
	}
	if numLines < 1 {
		numLines = 1
	}

	return labelLayout{
		HideBorder:    sheet.HideBorder,
		Cols:          cols,
		Rows:          rows,
		MarginX:       sheet.MarginLeft,
		MarginY:       sheet.MarginTop,
		LabelW:        labelW,
		LabelH:        labelH,
		LabelsPerPage: cols * rows,
		LeftW:         leftW,
		QRSize:        qrSize,
		QRTopPad:      qrTopPad,
		TextGap:       textGap,
		TextCellH:     textCellH,
		FontSize:      fontSize,
		NumLines:      numLines,
		LineSpacing:   lineSpacing,
	}, nil
}

func renderLabelsPDF(contents []labelContent, layout labelLayout) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetLineWidth(0.2)
	pdf.SetDrawColor(80, 80, 80)
	pdf.SetTextColor(20, 20, 20)

	const lineRight = 6.0

	for i, item := range contents {
		if i%layout.LabelsPerPage == 0 {
			pdf.AddPage()
		}
		pageIdx := i % layout.LabelsPerPage
		col := pageIdx % layout.Cols
		row := pageIdx / layout.Cols
		x := layout.MarginX + float64(col)*layout.LabelW
		y := layout.MarginY + float64(row)*layout.LabelH

		if !layout.HideBorder {
			pdf.Rect(x, y, layout.LabelW, layout.LabelH, "")
		}
		pdf.Line(x+layout.LeftW, y, x+layout.LeftW, y+layout.LabelH)

		// QR code, horizontally centered in the left cell
		qrX := x + (layout.LeftW-layout.QRSize)/2
		qrY := y + layout.QRTopPad
		pdf.ImageOptions(item.QRPath, qrX, qrY, layout.QRSize, layout.QRSize, false, gofpdf.ImageOptions{ImageType: "png"}, 0, "")

		// Primary text immediately below the QR code
		pdf.SetFont("Helvetica", "", layout.FontSize)
		textY := qrY + layout.QRSize + layout.TextGap
		pdf.SetXY(x+1, textY)
		pdf.CellFormat(layout.LeftW-2, layout.TextCellH, item.PrimaryText, "", 0, "C", false, 0, "")

		// Lines on the right — group centered vertically in the cell
		if layout.NumLines > 0 {
			spacing := layout.LineSpacing
			// cap spacing so lines never overflow the cell (5mm edge padding each side)
			if layout.NumLines > 1 {
				maxSpacing := (layout.LabelH - 10) / float64(layout.NumLines-1)
				if spacing > maxSpacing {
					spacing = maxSpacing
				}
			}
			blockH := float64(layout.NumLines-1) * spacing
			startY := y + (layout.LabelH-blockH)/2
			lineStartX := x + layout.LeftW + 4
			lineEndX := x + layout.LabelW - lineRight
			for li := 0; li < layout.NumLines; li++ {
				pdf.Line(lineStartX, startY+float64(li)*spacing, lineEndX, startY+float64(li)*spacing)
			}
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
