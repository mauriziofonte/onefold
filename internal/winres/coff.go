// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package winres

import (
	"bytes"
	"encoding/binary"
	"sort"
)

// Resource types and constants of the PE/COFF format.
const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtManifest  = 24
	langEnUS    = 0x0409

	imageFileMachineAMD64  = 0x8664
	imageRelAMD64Addr32NB  = 3 // 32-bit image-relative address: the linker adds the .rsrc RVA
	imageSymClassStatic    = 3
	sectionInitDataReadOnl = 0x40000040 // IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ
	subdirFlag             = 0x80000000
)

type resource struct {
	typ, id uint16
	data    []byte
}

// syso lays out the icon images (RT_ICON 1..n), their group (RT_GROUP_ICON
// IconID), the manifest (RT_MANIFEST 1) and, when given, the version
// information (RT_VERSION 1) as a resource tree (type -> id ->
// language -> data) in a one-section COFF object.
func syso(icons [][]byte, sizes []int, manifest, version []byte) []byte {
	var res []resource
	grp := new(bytes.Buffer)
	le := binary.LittleEndian
	binary.Write(grp, le, [3]uint16{0, 1, uint16(len(icons))}) // GRPICONDIR: reserved, type icon, count
	for i, ic := range icons {
		dim := uint8(sizes[i]) // 256 is written as 0
		if sizes[i] >= 256 {
			dim = 0
		}
		grp.Write([]byte{dim, dim, 0, 0})       // width, height, colors, reserved
		binary.Write(grp, le, [2]uint16{1, 32}) // planes, bits per pixel
		binary.Write(grp, le, uint32(len(ic)))  // bytes in resource
		binary.Write(grp, le, uint16(i+1))      // RT_ICON id
		res = append(res, resource{rtIcon, uint16(i + 1), ic})
	}
	res = append(res, resource{rtGroupIcon, IconID, grp.Bytes()}, resource{rtManifest, 1, manifest})
	if version != nil {
		res = append(res, resource{rtVersion, 1, version})
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].typ != res[j].typ {
			return res[i].typ < res[j].typ
		}
		return res[i].id < res[j].id
	})

	var types []uint16
	byType := map[uint16][]int{}
	for i, r := range res {
		if len(byType[r.typ]) == 0 {
			types = append(types, r.typ)
		}
		byType[r.typ] = append(byType[r.typ], i)
	}
	dirSize := func(n int) int { return 16 + 8*n }
	// offsets: root, type dirs, id dirs (one language each), data entries, data
	off := dirSize(len(types))
	typeOff := map[uint16]int{}
	for _, t := range types {
		typeOff[t] = off
		off += dirSize(len(byType[t]))
	}
	idOff := make([]int, len(res))
	for i := range res {
		idOff[i] = off
		off += dirSize(1)
	}
	entryOff := make([]int, len(res))
	for i := range res {
		entryOff[i] = off
		off += 16
	}
	dataOff := make([]int, len(res))
	for i, r := range res {
		off = align8(off)
		dataOff[i] = off
		off += len(r.data)
	}
	raw := make([]byte, align8(off))

	dir := func(at, entries int) { le.PutUint16(raw[at+14:], uint16(entries)) } // only id entries
	entry := func(at int, id uint16, target uint32) {
		le.PutUint32(raw[at:], uint32(id))
		le.PutUint32(raw[at+4:], target)
	}
	dir(0, len(types))
	for k, t := range types {
		entry(16+8*k, t, subdirFlag|uint32(typeOff[t]))
		dir(typeOff[t], len(byType[t]))
		for n, i := range byType[t] {
			entry(typeOff[t]+16+8*n, res[i].id, subdirFlag|uint32(idOff[i]))
		}
	}
	var relocs []uint32
	for i, r := range res {
		dir(idOff[i], 1)
		entry(idOff[i]+16, langEnUS, uint32(entryOff[i]))
		le.PutUint32(raw[entryOff[i]:], uint32(dataOff[i])) // OffsetToData: relocated to an RVA
		le.PutUint32(raw[entryOff[i]+4:], uint32(len(r.data)))
		relocs = append(relocs, uint32(entryOff[i]))
		copy(raw[dataOff[i]:], r.data)
	}

	const fileHeader, sectionHeader = 20, 40
	rawAt := fileHeader + sectionHeader
	relocAt := rawAt + len(raw)
	symAt := relocAt + 10*len(relocs)
	out := new(bytes.Buffer)
	binary.Write(out, le, struct {
		Machine, Sections                  uint16
		Time, SymbolTable, Symbols         uint32
		OptionalHeaderSize, Characteristic uint16
	}{imageFileMachineAMD64, 1, 0, uint32(symAt), 1, 0, 0})
	binary.Write(out, le, struct {
		Name                                      [8]byte
		VirtualSize, VirtualAddress, RawSize, Raw uint32
		Relocations, LineNumbers                  uint32
		RelocationCount, LineNumberCount          uint16
		Characteristics                           uint32
	}{[8]byte{'.', 'r', 's', 'r', 'c'}, 0, 0, uint32(len(raw)), uint32(rawAt), uint32(relocAt), 0,
		uint16(len(relocs)), 0, sectionInitDataReadOnl})
	out.Write(raw)
	for _, r := range relocs {
		binary.Write(out, le, struct {
			VirtualAddress, Symbol uint32
			Type                   uint16
		}{r, 0, imageRelAMD64Addr32NB})
	}
	binary.Write(out, le, struct {
		Name              [8]byte
		Value             uint32
		Section           int16
		Type              uint16
		StorageClass, Aux uint8
	}{[8]byte{'.', 'r', 's', 'r', 'c'}, 0, 1, 0, imageSymClassStatic, 0})
	binary.Write(out, le, uint32(4)) // empty string table
	return out.Bytes()
}

func align8(n int) int { return (n + 7) &^ 7 }
