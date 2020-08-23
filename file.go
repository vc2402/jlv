package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const bufSize = 1024
const cacheSize = 1024
const knownTagsDepth = 500

var buffer = make([]byte, bufSize)

type line struct {
	start  int64
	len    int
	cached *item
}
type item struct {
	m map[string]interface{}
	l *line
	n *item
	p *item
}
type cache struct {
	head *item
	tail *item
	len  int
}

//File - log file for parsing and viewing
type File struct {
	f         *os.File
	index     []line
	cache     cache
	err       error
	knownTags []string
	tagNames  []string
}

//FileView - view on File (filtered, sorted and so on)
type FileView struct {
	file   *File
	index  []int
	parent *FileView
	name   string
	pos    int
	err    error
}

type FilterOperator string

type Filter struct {
	Tag      string
	Mask     string
	Operator FilterOperator
}

const (
	FOEqual          FilterOperator = "eq"
	FONotEqual       FilterOperator = "ne"
	FOGreaterOrEqual FilterOperator = "ge"
	FOLessOrEqual    FilterOperator = "le"
	FORegexp         FilterOperator = "regexp"
)

//SearchDirection type for search functions
type SearchDirection int

const (
	//SearchForward - start search from given line forwards
	SearchForward = iota
	//SearchBack - start search from given line backwards
	SearchBack
)

const (
	LevelTraceName = "trace"
	LevelDebugName = "debug"
	LevelInfoName  = "info"
	LevelWarnName  = "warn"
	LevelErrorName = "error"
	LevelFaultName = "fault"
)

const (
	LevelTrace int = iota
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFault
)

var levels = [LevelFault + 1]string{
	LevelTraceName,
	LevelDebugName,
	LevelInfoName,
	LevelWarnName,
	LevelErrorName,
	LevelFaultName,
}

//well known tags
type Tag int

const (
	TagLevel Tag = iota
	TagTime
	TagMessage
	TagOther
)

func NewFile(f *os.File) (*File, error) {
	fl := &File{
		f:        f,
		tagNames: []string{"level", "time", "msg"},
	}
	pos := int64(0)
	length := 0
	f.Seek(pos, 0)
	for {
		l, err := f.Read(buffer)
		// fmt.Printf("read %d: \n%s)\n", l, buffer[:l])
		if l > 0 {
			for i := 0; i < l; i, length = i+1, length+1 {
				if buffer[i] == '\n' {
					fl.index = append(fl.index, line{start: pos, len: length})
					// fmt.Printf("line %d(%d:%d)\n", len(fl.index), pos, length)
					pos += int64(length + 1)
					length = -1
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fl, err
		}

	}
	for i := 0; i < knownTagsDepth && i < len(fl.index); i++ {
		fl.fillKnownTags(i)
	}
	fl.sortKnownTags()
	return fl, nil
}

func (f *File) View() *FileView {
	return &FileView{file: f}
}

func (f *File) LinesCount() int {
	return len(f.index)
}

func (f *FileView) Position() int {
	return f.pos
}

func (f *FileView) LinesCount() int {
	if f.index != nil {
		return len(f.index)
	}
	if f.parent != nil {
		return f.parent.LinesCount()
	}
	return f.file.LinesCount()
}
func (f *FileView) Err() error {
	if f.err != nil {
		return f.err
	}
	if f.parent != nil {
		return f.parent.Err()
	}
	return f.file.Err()
}

func (f *FileView) HaveParent() bool {
	return f.parent != nil
}

func (f *FileView) Parent() *FileView {
	if f.parent != nil {
		return f.parent
	}
	return f
}

// Up returns parent view with the same position
func (f *FileView) Up() *FileView {
	if f.parent == nil {
		return f
	}
	f.parent.rewindTo(f.pos)
	return f.parent
}

// Top returns top most view with the same position
func (f *FileView) Top() *FileView {
	if f.parent == nil {
		return f
	}
	p := f.parent
	for p.parent != nil {
		p = p.parent
	}
	p.rewindTo(f.pos)
	return p
}

func (f *FileView) Name() string {
	return f.name
}

func (f *FileView) KnownTags() []string {
	return f.file.knownTags
}

func (f *FileView) KnownTag(tag string) {
	f.file.addKnownTag(tag)
}

func (f *FileView) AddKnownTags(m map[string]interface{}) {
	for t := range m {
		f.KnownTag(t)
	}
}

func (f *FileView) Levels() []string {
	return levels[:]
}

func (f *FileView) Filter(fltr Filter) *FileView {
	ret := &FileView{parent: f, file: f.file, name: fltr.String(), index: []int{}}
	for i := 0; i < f.LinesCount(); i++ {
		it := f.item(i)
		if it != nil && f.file.fit(it, fltr) {
			ret.index = append(ret.index, f.getIndex(i))
		}
	}
	return ret
}

func (f *FileView) AbsLine(idx int) map[string]interface{} {
	it := f.item(idx)
	if it == nil {
		return nil
	}
	return it.m
}

func (f *FileView) Line(n int) map[string]interface{} {
	return f.AbsLine(n + f.pos)
}

func (f *FileView) TagName(tag Tag) string {
	return f.file.TagName(tag)
}

func (f *FileView) Level(m map[string]interface{}) int {
	return f.file.Level(m)
}

func (f *FileView) LevelName(m map[string]interface{}) string {
	return f.file.LevelName(m)
}

//Move moves current position to lines lines from the current
func (f *FileView) Move(lines int) *FileView {
	f.pos += lines
	return f
}

//SetPosition sets absolute position of view
func (f *FileView) SetPosition(pos int) *FileView {
	f.pos = pos
	return f
}

//Search looks for mask in view forwards or backwards from the given line including it
//  returns found line's index or -1 if none
//  Search looks for mask in whole file lines, not in tags
func (f *FileView) Search(mask string, from int, direction SearchDirection, regexp ...bool) (int, error) {
	checkIdx := func() {
		if from >= f.len() {
			from = 0
		} else if from < 0 {
			from = f.len() - 1
		}
	}
	checkIdx()
	start := from
	for {
		b := f.file.bytes(f.getIndex(from))
		if b == nil {
			return -1, errors.New("file read error")
		}
		if strings.Index(string(b), mask) != -1 {
			return from, nil
		}
		if direction == SearchForward {
			from++
		} else {
			from--
		}
		if start == from {
			return -1, nil
		}
		checkIdx()
	}
}

//SearchTag looks for mask in view forwards or backwards from the given line including it
//  returns found line's index or -1 if none
//  Search looks for mask in given tags only
func (f *FileView) SearchTag(tag string, mask string, from int, direction SearchDirection, regexp ...bool) (int, error) {
	checkIdx := func() {
		if from >= f.len() {
			from = 0
		} else if from < 0 {
			from = f.len() - 1
		}
	}
	checkIdx()
	start := from
	for {
		it := f.file.item(f.getIndex(from))
		if it == nil {
			return -1, errors.New("file read error")
		}
		if t, ok := it.m[tag]; ok && strings.Index(tagToString(t), mask) != -1 {
			return from, nil
		}
		if direction == SearchForward {
			from++
		} else {
			from--
		}
		if start == from {
			return -1, nil
		}
		checkIdx()
	}
}

func (f *File) Line(n int) map[string]interface{} {
	it := f.item(n)
	if it == nil {
		return nil
	}
	return it.m
}

func (f *File) TagName(tag Tag) string {
	return f.tagNames[tag]
}

func (f *File) Level(m map[string]interface{}) int {
	lev := f.LevelName(m)
	for l, ln := range levels {
		if ln == lev {
			return l
		}
	}
	return -1
}

func (f *File) LevelName(m map[string]interface{}) string {
	if l, ok := m[f.tagNames[TagLevel]].(string); ok {
		return strings.ToLower(l)
	}
	return ""
}

func (f *File) Err() error {
	return f.err
}

func (f Filter) String() string {
	return fmt.Sprintf("%s %s %s", f.Tag, f.Operator, f.Mask)
}
func (f *FileView) item(idx int) *item {
	if f.index != nil {
		if idx < 0 || idx > len(f.index)-1 {
			return nil
		}
		return f.file.item(f.index[idx])
	}
	return f.file.item(idx)
}

func (f *File) item(n int) *item {
	if n < 0 || n >= len(f.index) {
		return nil
	}
	l := f.index[n]
	if l.cached != nil {
		return l.cached
	}
	buf := f.bytes(n)
	it := f.cache.item(&l)
	f.err = json.Unmarshal(buf, &it.m)
	return it
}

func (f *File) bytes(n int) []byte {
	if n < 0 || n >= len(f.index) {
		return nil
	}
	l := f.index[n]
	f.f.Seek(l.start, 0)
	if len(buffer) < l.len {
		buffer = make([]byte, l.len)
	}
	_, f.err = f.f.Read(buffer[:l.len])
	if f.err != nil {
		return nil
	}
	return buffer[:l.len]
}

func (f *File) fit(it *item, q Filter) bool {
	//TODO correctly process not strings (especially numbers)
	if q.Tag != "" {
		if t, ok := it.m[q.Tag]; ok {
			val := tagToString(t)
			islevel := q.Tag == f.TagName(TagLevel)
			lev := -1
			reqLev := -1
			if islevel {
				lev = f.decodeLevel(val)
				reqLev = f.decodeLevel(q.Mask)
			}
			switch q.Operator {
			case FOEqual:
				if islevel {
					return lev == reqLev
				}
				return val == q.Mask
			case FONotEqual:
				if islevel {
					return lev != reqLev
				}
				return val != q.Mask
			case FOGreaterOrEqual:
				if islevel {
					return lev >= reqLev
				}
				return val >= q.Mask
			case FOLessOrEqual:
				if islevel {
					return lev <= reqLev
				}
				return val <= q.Mask
			case FORegexp:
				match, err := regexp.MatchString(q.Mask, val)
				if err != nil {
					f.err = err
				}
				return match
			}
		}
	}
	return false
}

func (f *File) decodeLevel(lev string) int {
	lev = strings.ToLower(lev)
	for l, ln := range levels {
		if ln == lev {
			return l
		}
	}
	return -1
}

func (f *File) decodeTag(tag string) Tag {
	t := TagLevel
	for ; t < TagOther; t++ {
		if tag == f.tagNames[t] {
			break
		}
	}
	return t
}

func (f *File) fillKnownTags(n int) {
	it := f.item(n)
	for tag := range it.m {
		f.addKnownTag(tag)
	}
}

func (f *File) addKnownTag(tag string) {
	found := false
	for _, t := range f.knownTags {
		if t == tag {
			found = true
			break
		}
	}
	if !found {
		f.knownTags = append(f.knownTags, tag)
	}
}

func (f *File) sortKnownTags() {
	tags := make([]string, len(f.knownTags))
	pos := 3
	for _, t := range f.knownTags {
		tag := f.decodeTag(t)
		switch tag {
		case TagTime:
			tags[0] = t
		case TagLevel:
			tags[1] = t
		case TagMessage:
			tags[2] = t
		default:
			tags[pos] = t
			pos++
		}
	}
	f.knownTags = tags
}

func (f *FileView) getIndex(i int) int {
	if f.index != nil {
		return f.index[i]
	}
	return i
}

func (f *FileView) len() int {
	if f.index != nil {
		return len(f.index)
	}
	return len(f.file.index)
}

func (f *FileView) rewindTo(idx int) {
	if f.index != nil {
		for i, pos := range f.index {
			if pos >= idx {
				f.pos = i
				break
			}
		}
		return
	}
	f.pos = idx
}

func (c cache) item(forLine *line) *item {
	if c.len < cacheSize {
		c.head = &item{n: c.head, m: map[string]interface{}{}, l: forLine}
		if c.tail == nil {
			c.tail = c.head
		}
		c.len++
	} else {
		it := c.tail
		c.tail = c.head.p
		c.tail.n = nil
		it.n = c.head
		c.head = it
		it.p = nil
		it.l = forLine
	}
	forLine.cached = c.head
	return c.head
}

func tagToString(tag interface{}) string {
	switch val := tag.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", tag)
	}
}
