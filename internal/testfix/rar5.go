// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package testfix

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"strings"
)

// RAR5 writes a RAR 5.0 archive with the members stored (method 0, no
// compression), built from the published format: signature, main header,
// one file header per member (Unix mtime, data CRC32), end-of-archive.
// A member whose name ends in "/" is a directory. ModTime is an instant:
// RAR5 keeps UTC.
func RAR5(p string, ms []Member) error {
	if err := mkdir(p); err != nil {
		return err
	}
	var out bytes.Buffer
	out.Write([]byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00})
	block := func(typ, flags uint64, fields []byte, data []byte) {
		var h bytes.Buffer
		h.Write(vint(typ))
		h.Write(vint(flags))
		if flags&0x2 != 0 {
			h.Write(vint(uint64(len(data))))
		}
		h.Write(fields)
		size := vint(uint64(h.Len()))
		crc := crc32.NewIEEE()
		crc.Write(size)
		crc.Write(h.Bytes())
		var c [4]byte
		binary.LittleEndian.PutUint32(c[:], crc.Sum32())
		out.Write(c[:])
		out.Write(size)
		out.Write(h.Bytes())
		out.Write(data)
	}
	block(1, 0, vint(0), nil) // main archive header, no archive flags
	for _, m := range ms {
		dir := strings.HasSuffix(m.Name, "/")
		name := strings.TrimSuffix(m.Name, "/")
		data := []byte(m.Body)
		var f bytes.Buffer
		fileFlags := uint64(0x2 | 0x4) // mtime, CRC32
		attrs := uint64(0o100644)
		if dir {
			fileFlags, attrs, data = 0x1|0x2, 0o40755, nil
		}
		f.Write(vint(fileFlags))
		f.Write(vint(uint64(len(data)))) // unpacked size
		f.Write(vint(attrs))
		var t [4]byte
		binary.LittleEndian.PutUint32(t[:], uint32(m.ModTime.Unix()))
		f.Write(t[:])
		if !dir {
			var c [4]byte
			binary.LittleEndian.PutUint32(c[:], crc32.ChecksumIEEE(data))
			f.Write(c[:])
		}
		f.Write(vint(0)) // compression: version 0, method 0 (store)
		f.Write(vint(1)) // host OS: Unix
		f.Write(vint(uint64(len(name))))
		f.WriteString(name)
		flags := uint64(0)
		if !dir {
			flags = 0x2 // data area present
		}
		block(2, flags, f.Bytes(), data)
	}
	block(5, 0, vint(0), nil) // end of archive
	return os.WriteFile(p, out.Bytes(), 0o644)
}

// vint is the RAR5 variable-length integer: 7 bits per byte, low first,
// high bit = more bytes follow.
func vint(v uint64) []byte {
	var b []byte
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b = append(b, c|0x80)
			continue
		}
		return append(b, c)
	}
}
