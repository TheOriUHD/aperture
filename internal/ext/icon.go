package ext

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/png" // register PNG for image.DecodeConfig
)

const (
	// MaxIconDim / MaxIconBytes bound a pinned extension's nav-rail icon. Small on
	// purpose: the icon is host-chrome, not content.
	MaxIconDim   = 256
	MaxIconBytes = 64 << 10 // 64 KiB
)

// ValidateIcon enforces that a pinned extension's icon is a genuine, small PNG —
// NOT merely a file with a .png name. The rail renders it as an <img> (never
// inlined into host DOM), and this install-time check is the belt to that
// suspenders: a bundle that ships e.g. an SVG containing <script> under the name
// icon.png is rejected here because image.DecodeConfig only recognises it as PNG
// when it is actually PNG, so hostile markup can never reach the host as an icon.
func ValidateIcon(b []byte) error {
	if len(b) == 0 {
		return errors.New("icon file is empty")
	}
	if len(b) > MaxIconBytes {
		return fmt.Errorf("icon is %d bytes; the maximum is %d", len(b), MaxIconBytes)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("icon is not a decodable image: %w", err)
	}
	if format != "png" {
		return fmt.Errorf("icon must be a PNG, not %s", format)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > MaxIconDim || cfg.Height > MaxIconDim {
		return fmt.Errorf("icon must be between 1x1 and %dx%d px, got %dx%d", MaxIconDim, MaxIconDim, cfg.Width, cfg.Height)
	}
	return nil
}
