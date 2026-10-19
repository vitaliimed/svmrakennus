package site

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

func checkImage(b []byte, kind string) error {
	var width, height int
	if kind != "image/webp" {
		c, _, e := image.DecodeConfig(bytes.NewReader(b))
		if e != nil {
			return errors.New("image header is invalid")
		}
		width, height = c.Width, c.Height
	} else {
		if len(b) < 30 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
			return errors.New("WebP header is invalid")
		}
		switch string(b[12:16]) {
		case "VP8X":
			width = 1 + int(b[24]) + int(b[25])<<8 + int(b[26])<<16
			height = 1 + int(b[27]) + int(b[28])<<8 + int(b[29])<<16
		case "VP8L":
			if b[20] != 0x2f {
				return errors.New("WebP lossless header is invalid")
			}
			n := binary.LittleEndian.Uint32(b[21:25])
			width = int(n&0x3fff) + 1
			height = int((n>>14)&0x3fff) + 1
		case "VP8 ":
			if string(b[23:26]) != "\x9d\x01\x2a" {
				return errors.New("WebP frame header is invalid")
			}
			width = int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff)
			height = int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff)
		default:
			return errors.New("unsupported WebP image")
		}
	}
	if width < 1 || height < 1 || width > 20000 || height > 20000 || int64(width)*int64(height) > 100000000 {
		return errors.New("image dimensions are too large or invalid")
	}
	return nil
}
