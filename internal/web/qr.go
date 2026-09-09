package web

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// QR code rendering for short URLs.

type QRFormat int

const (
	QRPNG QRFormat = iota
	QRSVG
)

type QROptions struct {
	Size            int
	Margin          int
	ErrorCorrection qrcode.RecoveryLevel
	Format          QRFormat
}

func DefaultQROptions() QROptions {
	return QROptions{Size: 300, Margin: 1, ErrorCorrection: qrcode.Low, Format: QRPNG}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func ParseQROptions(size, margin *int, level, format string) QROptions {
	opts := DefaultQROptions()
	if size != nil {
		opts.Size = clamp(*size, 50, 1000)
	}
	if margin != nil {
		opts.Margin = clamp(*margin, 0, 20)
	}
	switch strings.ToUpper(level) {
	case "M":
		opts.ErrorCorrection = qrcode.Medium
	case "Q":
		opts.ErrorCorrection = qrcode.High
	case "H":
		opts.ErrorCorrection = qrcode.Highest
	}
	if strings.ToLower(format) == "svg" {
		opts.Format = QRSVG
	}
	return opts
}

// qrModules yields the QR module matrix without a quiet zone; the margin is
// applied by the renderers.
func qrModules(content string, level qrcode.RecoveryLevel) ([][]bool, error) {
	qr, err := qrcode.New(content, level)
	if err != nil {
		return nil, err
	}
	qr.DisableBorder = true
	return qr.Bitmap(), nil
}

// RespondQr renders a QR code for the given content as an HTTP response.
func RespondQR(w http.ResponseWriter, content string, opts QROptions) {
	modules, err := qrModules(content, opts.ErrorCorrection)
	if err != nil {
		http.Error(w, "could not generate QR code", http.StatusInternalServerError)
		return
	}
	n := len(modules)
	total := n + opts.Margin*2

	switch opts.Format {
	case QRSVG:
		var b strings.Builder
		fmt.Fprintf(&b,
			`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges">`,
			opts.Size, opts.Size, total, total)
		fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#ffffff"/>`, total, total)
		for y, row := range modules {
			for x, filled := range row {
				if filled {
					fmt.Fprintf(&b, `<rect x="%d" y="%d" width="1" height="1" fill="#000000"/>`,
						x+opts.Margin, y+opts.Margin)
				}
			}
		}
		b.WriteString("</svg>")
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(b.String()))

	default:
		// Module count + margin determines pixels-per-module for the
		// requested size.
		ppm := opts.Size / total
		if ppm < 1 {
			ppm = 1
		}
		dim := total * ppm
		img := image.NewGray(image.Rect(0, 0, dim, dim))
		for i := range img.Pix {
			img.Pix[i] = 0xFF
		}
		for y, row := range modules {
			for x, filled := range row {
				if !filled {
					continue
				}
				for dy := 0; dy < ppm; dy++ {
					for dx := 0; dx < ppm; dx++ {
						img.SetGray((x+opts.Margin)*ppm+dx, (y+opts.Margin)*ppm+dy, color.Gray{Y: 0})
					}
				}
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			http.Error(w, "could not encode QR code", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(buf.Bytes())
	}
}
