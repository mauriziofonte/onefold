// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"path/filepath"
	"sort"
	"strings"
)

// ItemQuery selects a page of a result list: search text, extension of
// the source, folder of the source (tree view), order.
type ItemQuery struct {
	List   string
	Q      string
	Ext    string // lowercase, no dot; "" = any
	Dir    string // only sources inside this folder (same form as Src)
	Sort   string // src, dst, size; "-" prefix = descending; "" = list order
	Offset int
	Limit  int
}

// TreeNode is one entry of a list grouped by source folder.
type TreeNode struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Dir   bool   `json:"dir"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
	Note  string `json:"note,omitempty"`
	Dst   string `json:"dst,omitempty"`
}

const treeMaxFiles = 2000

func (q ItemQuery) keep(it Item) bool {
	if q.Q != "" && !strings.Contains(strings.ToLower(it.Src+"\x00"+it.Dst+"\x00"+it.Note), q.Q) {
		return false
	}
	if q.Ext != "" && strings.ToLower(strings.TrimPrefix(filepath.Ext(it.Src), ".")) != q.Ext {
		return false
	}
	return q.Dir == "" || within(it.Src, q.Dir)
}

// within reports whether p is inside dir (component boundary, OS case rule).
func within(p, dir string) bool {
	fp, fd := foldPath(p), foldPath(dir)
	return strings.HasPrefix(fp, strings.TrimSuffix(fd, string(filepath.Separator))+string(filepath.Separator))
}

func (m *Manager) loadList(id, list string) ([]Item, error) {
	if _, ok := m.Get(id); !ok {
		return nil, errNotFound
	}
	var r result
	if err := m.st.loadResult(id, &r); err != nil {
		return nil, nil
	}
	return r.Lists[list], nil
}

// query returns a page of a result list and the number of matching items.
func (m *Manager) query(id string, q ItemQuery) ([]Item, int, error) {
	all, err := m.loadList(id, q.List)
	if err != nil {
		return nil, 0, err
	}
	q.Q, q.Ext = strings.ToLower(q.Q), strings.ToLower(strings.TrimPrefix(q.Ext, "."))
	var sel []Item
	for _, it := range all {
		if q.keep(it) {
			sel = append(sel, it)
		}
	}
	if s := strings.TrimPrefix(q.Sort, "-"); s != "" {
		desc := strings.HasPrefix(q.Sort, "-")
		key := func(it Item) string {
			if s == "dst" {
				return strings.ToLower(it.Dst)
			}
			return strings.ToLower(it.Src)
		}
		sort.SliceStable(sel, func(a, b int) bool {
			if s == "size" {
				if desc {
					return sel[a].Size > sel[b].Size
				}
				return sel[a].Size < sel[b].Size
			}
			if desc {
				return key(sel[a]) > key(sel[b])
			}
			return key(sel[a]) < key(sel[b])
		})
	}
	total := len(sel)
	if q.Offset >= total {
		return []Item{}, total, nil
	}
	end := min(q.Offset+q.Limit, total)
	return sel[q.Offset:end], total, nil
}

// exts lists the source extensions of a list with their counts.
func (m *Manager) exts(id, list string) (map[string]int, error) {
	all, err := m.loadList(id, list)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, it := range all {
		out[strings.ToLower(strings.TrimPrefix(filepath.Ext(it.Src), "."))]++
	}
	return out, nil
}

// tree groups the items of a list (matching q) by source folder: at the top
// the folders the sources live in (rooms), below dir its sub-folders and the
// files directly in it (at most treeMaxFiles, biggest first).
func (m *Manager) tree(id string, q ItemQuery, rooms []string) ([]TreeNode, bool, error) {
	all, err := m.loadList(id, q.List)
	if err != nil {
		return nil, false, err
	}
	q.Q, q.Ext = strings.ToLower(q.Q), strings.ToLower(strings.TrimPrefix(q.Ext, "."))
	dir := q.Dir
	q.Dir = ""
	sep := string(filepath.Separator)
	folders := map[string]*TreeNode{}
	var files []TreeNode
	add := func(path, name string, it Item) {
		n := folders[foldPath(path)]
		if n == nil {
			n = &TreeNode{Name: name, Path: path, Dir: true}
			folders[foldPath(path)] = n
		}
		n.Files++
		n.Bytes += it.Size
	}
	for _, it := range all {
		if !q.keep(it) {
			continue
		}
		if dir == "" {
			if room := roomOf(it.Src, rooms); room != "" {
				add(room, room, it)
				continue
			}
			// relative sources (rearrange): their first component
			first, _, ok := strings.Cut(it.Src, sep)
			if !ok {
				files = append(files, TreeNode{Name: it.Src, Path: it.Src, Files: 1, Bytes: it.Size, Note: it.Note, Dst: it.Dst})
				continue
			}
			add(first, first, it)
			continue
		}
		if !within(it.Src, dir) {
			continue
		}
		rel := strings.TrimPrefix(it.Src[len(strings.TrimSuffix(dir, sep)):], sep)
		first, _, ok := strings.Cut(rel, sep)
		if !ok {
			files = append(files, TreeNode{Name: rel, Path: it.Src, Files: 1, Bytes: it.Size, Note: it.Note, Dst: it.Dst})
			continue
		}
		add(strings.TrimSuffix(dir, sep)+sep+first, first, it)
	}
	out := make([]TreeNode, 0, len(folders)+len(files))
	for _, n := range folders {
		out = append(out, *n)
	}
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	sort.SliceStable(files, func(a, b int) bool { return files[a].Bytes > files[b].Bytes })
	more := len(files) > treeMaxFiles
	if more {
		files = files[:treeMaxFiles]
	}
	return append(out, files...), more, nil
}

// roomOf returns the longest room holding p ("" if none).
func roomOf(p string, rooms []string) string {
	best := ""
	for _, r := range rooms {
		if within(p, r) && len(r) > len(best) {
			best = r
		}
	}
	return best
}

// summaryRooms reads the "rooms" of a job summary (JSON-decoded or not).
func summaryRooms(sum map[string]any) []string {
	switch v := sum["rooms"].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
