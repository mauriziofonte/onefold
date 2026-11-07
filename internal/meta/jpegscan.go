// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/evanoberholster/imagemeta/imagetype"
	"github.com/evanoberholster/imagemeta/jpeg"
	imeta "github.com/evanoberholster/imagemeta/meta"
	"github.com/evanoberholster/imagemeta/meta/utils"
)

// scanJPEG is jpeg.ScanJPEG of imagemeta v0.3.1 (MIT, Copyright (c)
// 2019-2023 Evan Oberholster), the same marker walk step for step, with one
// difference: where the original loops forever, scanJPEG stops with
// errJPEGStray. The original spins when it meets a marker while it is
// outside any image: before the first start-of-image, or after a top-level
// end-of-image (FF D9 found before the quantization tables, which only a
// damaged file has). One such photo would stop a whole scan. On every other
// input the two return the same error and call the readers on the same
// bytes (TestScanJPEGMatchesImagemeta, FuzzJPEG).
func scanJPEG(r io.Reader, exifReader func(io.Reader, imeta.ExifHeader) error, xmpReader func(io.Reader) error) (err error) {
	defer func() {
		if state := recover(); state != nil {
			if e, ok := state.(error); ok {
				err = e
			} else {
				err = fmt.Errorf("malformed JPEG (%v)", state)
			}
		}
	}()
	br, ok := r.(*bufio.Reader)
	if !ok || br.Size() < jpegBufferSize {
		br = bufio.NewReaderSize(r, jpegBufferSize)
	}
	jr := &jpegWalk{br: br, exifReader: exifReader, xmpReader: xmpReader}
	// The original also has a DHT case (stop at pos 1) and an SOI case, both
	// unreachable: DHT (C4) falls in the SOF range and nextMarker consumes
	// every SOI. They are left out.
	for jr.nextMarker() {
		switch jr.marker >> 4 {
		case 12: // C0-CF: SOF, DHT, DAC
			jr.err = jr.discard(int(jr.size) + 2)
		case 14: // APP
			if jr.marker == 0xE1 {
				jr.readAPP1()
			} else {
				jr.skip()
			}
		default:
			switch jr.marker {
			case 0xD9: // EOI
				jr.pos--
				if jr.pos == 1 {
					return jpeg.ErrEndOfImage
				}
				jr.err = jr.discard(2)
			case 0xDB: // DQT: stop
				jr.skip()
				return nil
			case 0xDD: // DRI
				jr.err = jr.discard(6)
			default:
				jr.skip()
			}
		}
	}
	return jr.err
}

// errJPEGStray: a marker outside any image, where imagemeta's walk never ends.
var errJPEGStray = errors.New("malformed JPEG: a marker outside the image")

const (
	jpegBufferSize   = 4 * 1024 // imagemeta's bufferSize
	exifPrefixLength = 8
	xmpPrefixLength  = 29
)

type jpegWalk struct {
	exifReader func(io.Reader, imeta.ExifHeader) error
	xmpReader  func(io.Reader) error
	br         *bufio.Reader
	err        error
	buf        []byte
	size       uint16
	marker     byte
	pos        uint8 // nesting depth of start-of-image markers (uint8 as in the original)
	discarded  uint32
}

func (jr *jpegWalk) nextMarker() bool {
	for jr.err == nil {
		if jr.buf, jr.err = jr.br.Peek(64); jr.err != nil {
			jr.err = jpeg.ErrNoJPEGMarker
			return false
		}
		if jr.buf[0] != 0xFF {
			i := 0
			for ; i < 64; i++ {
				if jr.buf[i] == 0xFF {
					break
				}
			}
			jr.err = jr.discard(i)
			continue
		}
		if jr.buf[1] == 0xD8 {
			jr.pos++
			jr.err = jr.discard(2)
			continue
		}
		if jr.pos > 0 {
			jr.size = binary.BigEndian.Uint16(jr.buf[2:4])
			jr.marker = jr.buf[1]
			return true
		}
		// imagemeta loops here forever: nothing is discarded and the state
		// cannot change.
		jr.err = errJPEGStray
	}
	return false
}

func (jr *jpegWalk) discard(i int) (err error) {
	if i == 0 {
		return nil
	}
	i, err = jr.br.Discard(i)
	jr.discarded += uint32(i)
	return err
}

func (jr *jpegWalk) skip() { jr.err = jr.discard(int(jr.size) + 2) }

func (jr *jpegWalk) readAPP1() {
	switch {
	case string(jr.buf[4:10]) == "Exif\x00\x00":
		jr.err = jr.readExif()
	case string(jr.buf[4:33]) == "http://ns.adobe.com/xap/1.0/\x00":
		jr.err = jr.readXMP()
	default:
		jr.skip()
	}
}

func (jr *jpegWalk) readExif() (err error) {
	remain := int(jr.size) - exifPrefixLength
	if err = jr.discard(2 + exifPrefixLength); err != nil {
		return err
	}
	buf, err := jr.br.Peek(exifPrefixLength)
	if err != nil {
		return err
	}
	if jr.exifReader != nil {
		bo := utils.BinaryOrder(buf)
		h := imeta.NewExifHeader(bo, bo.Uint32(buf[4:8]), jr.discarded, uint32(remain), imagetype.ImageJPEG)
		if err = jr.exifReader(jr.br, h); err != nil {
			return err
		}
		remain = 0
	}
	return jr.discard(remain)
}

func (jr *jpegWalk) readXMP() (err error) {
	remain := int(jr.size) - 2 - xmpPrefixLength
	if err = jr.discard(4 + xmpPrefixLength); err != nil {
		return err
	}
	if jr.xmpReader != nil {
		r := io.LimitReader(jr.br, int64(remain))
		if err = jr.xmpReader(r); err != nil {
			return err
		}
		remain = int(r.(*io.LimitedReader).N)
	}
	return jr.discard(remain)
}
