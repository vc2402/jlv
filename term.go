package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/crypto/ssh/terminal"
)

const (
	keyUp    = "\033\133\101"
	keyDown  = "\033\133\102"
	keyRight = "\033\133\103"
	keyLeft  = "\033\133\104"
	keyPgUp  = "\033\133\065\176"
	keyPgDn  = "\033\133\066\176"
	keyHome  = "\033\133\110"
	keyEnd   = "\033\133\106"

	keyEnter     = 13
	keyBackspace = 127
	keyEsc       = 27
	keyTab       = 9
)

const (
	bgBlack   = 40
	bgRed     = 41
	bgGreen   = 42
	bgYellow  = 43
	bgBlue    = 44
	bgMagenta = 45
	bgCyan    = 46
	bgWhite   = 47
	bgDefault = 49

	fgBlack   = 30
	fgRed     = 31
	fgGreen   = 32
	fgYellow  = 33
	fgBlue    = 34
	fgMagenta = 35
	fgCyan    = 36
	fgWhite   = 37
	fgDefault = 39

	bold     = "\033[1m"
	reset    = "\033[0m"
	scrollUp = "\033[1S"
	scrollDn = "\033[1T"

	templBold     = "\033[1m%s\033[0m"
	templBoldSuff = "\033[1m%s\033[0m%s"
	templBoldPref = "%s\033[1m%s\033[0m"
	templBoldFull = "%s\033[1m%s\033[0m%s"
)

const (
	modeNormal = iota
	modeRecord
)

type option struct {
	name    string
	command string
	visible bool
}
type options struct {
	options []option
	current int
	replace bool
	prefix  string
}
type searchParams struct {
	mask     string
	idx      int
	dir      SearchDirection
	tag      string
	isRegexp bool
}
type term struct {
	f          *FileView
	t          *os.File
	w          int
	h          int
	exit       bool
	current    int
	command    string
	message    string
	mode       int
	lastSearch searchParams
	commands   map[string]*command
	*options
}

type command struct {
	name      string
	optionsFn func(*term)
	execFn    func(*term)
}

func startTerm(file *FileView) error {
	f := os.Stdin
	d := int(f.Fd())
	if !terminal.IsTerminal(d) {
		return errors.New("not a terminal")
	}
	s, err := terminal.MakeRaw(d)
	if err != nil {
		return err
	}
	w, h, err := terminal.GetSize(d)
	term := &term{f: file, t: f, w: w, h: h, commands: map[string]*command{}}
	term.fillCommands()
	buf := make([]byte, 4)
	term.redraw()

	for !term.exit {
		suff := fmt.Sprintf("%s %d(%d)", term.f.Name(), term.current+term.f.Position()+1, file.LinesCount())
		term.goTo(h, w-len(suff))
		term.write(suff)
		l, err := term.t.Read(buf)
		if err != nil {
			return err
		}
		term.processCommand(buf, l)
		term.goTo(h, 1)
		term.clearLine()
		if term.options != nil {
			term.showOptions()
		} else if term.command != "" {
			term.write(term.command)
		} else if term.message != "" {
			term.write(term.message)
		} else {
			term.write(fmt.Sprintf("read: %d bytes: %v", l, buf[:l]))
		}
	}
	defer terminal.Restore(d, s)
	return nil
}

func (t *term) redraw() {
	switch t.mode {
	case modeNormal:
		t.clear()
		max := t.h - 1
		if max > t.f.LinesCount()-t.f.Position() {
			max = t.f.LinesCount() - t.f.Position()
		}
		for i := 0; i < max; i++ {
			t.drawLine(i)
		}
	case modeRecord:
		t.showCurrent()
	}
}

func (t *term) drawLine(n int) {
	t.goTo(n+1, 1)
	t.clearLine()
	if t.current == n {
		t.setColor(fgBlack, bgWhite)
	}
	m := t.f.Line(n)
	lev := t.f.Level(m)
	if lev == LevelInfo {
		fg := fgGreen
		bg := bgDefault
		if t.current == n {
			fg, bg = fgBlack, bgGreen
		}
		t.setColor(fg, bg)
	}
	if m != nil {
		buff := strings.Builder{}
		buff.WriteString(fmt.Sprintf("%s %5s %s", m["time"], t.f.LevelName(m), m["msg"]))
		tags := t.f.KnownTags()
		found := 0
		for t := 3; t < len(tags); t++ {
			if v, ok := m[tags[t]]; ok {
				buff.WriteString(fmt.Sprintf("; %s: %v", tags[t], v))
				found++
			}
		}
		if found+3 < len(m) {
			t.f.AddKnownTags(m)
		}
		t.write(buff.String())
	}
	t.resetColor()

}

func (t *term) showOptions() {
	t.goTo(t.h, 1)
	t.clearLine()
	pref := t.options.prefix
	count := 0
	for i, o := range t.options.options {
		n := o.name
		if pref != "" && strings.Index(n, pref) == -1 {
			t.options.options[i].visible = false
			continue
		}
		t.options.options[i].visible = true
		if t.options.current == -1 {
			t.options.current = i
		}
		count++
		if i == t.options.current {
			col := fmt.Sprintf("\033[%d;%dm", fgBlack, bgWhite)
			idx := strings.Index(n, reset)
			if idx >= 0 && idx+len(reset) < len(n) {
				n = fmt.Sprintf("%s%s%s", n[:idx+len(reset)], col, n[idx+len(reset):])
			}
			n = fmt.Sprintf("%s%s%s", col, n, reset)
		}
		t.write(n)
		t.write(" ")
	}
	if count == 1 {
		t.selectCurrentOption()
	}
}

func (t *term) selectCurrentOption() {
	if t.options.current == -1 || t.options.current >= len(t.options.options) {
		return
	}
	pref := ""
	if !t.options.replace {
		pref = t.command
	}
	t.command = pref + t.options.options[t.options.current].command
	t.options = nil
}

func (t *term) processCommand(cmd []byte, length int) {
	t.message = ""
	if t.options != nil {
		if length == 1 {
			switch cmd[0] {
			case keyEsc:
				t.options = nil
			case keyEnter:
				t.selectCurrentOption()
			case keyBackspace:
				if t.options.prefix != "" {
					t.options.prefix = t.options.prefix[:len(t.options.prefix)-1]
					t.options.current = -1
					t.showOptions()
				}
			default:
				if unicode.IsPrint(rune(cmd[0])) {
					t.options.prefix += string(cmd[:1])
					t.options.current = -1
					t.showOptions()
				}
			}
		} else {
			switch string(cmd[:length]) {
			case keyLeft:
				t.options.prev()
			case keyRight:
				t.options.next()
			}
		}
		return
	}

	if t.command != "" && unicode.IsPrint(rune(cmd[0])) {
		t.command += string(cmd[:1])
		return
	}
	if length == 1 {
		switch cmd[0] {
		case keyTab:
			t.fillOptions()
		case keyBackspace:
			if t.command != "" {
				t.command = t.command[:len(t.command)-1]
			}
		case 'j':
			t.down()
		case 'k':
			t.up()
		case 'n':
			t.search(false)
		case 'N':
			t.search(true)
		case ':', '/', '?':
			t.command = string(cmd[:1])
		case keyEnter:
			if t.command != "" {
				t.execute()
			} else {
				switch t.mode {
				case modeNormal:
					t.mode = modeRecord
				case modeRecord:
					t.mode = modeNormal
				}
				t.redraw()
			}

		}
	} else if length >= 3 {
		switch string(cmd[:length]) {
		case keyUp:
			t.up()
		case keyDown:
			t.down()
		case keyHome:
			t.home()
		case keyEnd:
			t.end()
		case keyPgUp:
			t.pgUp()
		case keyPgDn:
			t.pgDn()
		}
	}
}
func (t *term) fillOptions() {
	c := t.findCommand()
	if c != nil {
		if c.optionsFn != nil {
			c.optionsFn(t)
		}
		return
	}
	t.options = &options{replace: true}
	for cl, c := range t.commands {
		l := len(t.command)
		if l > len(cl) {
			l = len(cl)
		}
		if c.name != "" &&
			(l == 0 || cl[:l] == t.command) {
			t.options.add(c.name, cl)
		}
	}
}

func (t *term) up() {
	if t.f.Position() > 0 && t.current <= t.h/2 {
		t.write(scrollDn)
		t.f.Move(-1)
		t.drawLine(t.current + 1)
		t.drawLine(t.current)
		t.drawLine(0)
	} else if t.current > 0 {
		curr := t.current
		t.current--
		t.drawLine(curr)
		t.drawLine(t.current)
	}
}
func (t *term) down() {
	if t.f.Position()+t.h-2 < t.f.LinesCount()-1 && t.current >= t.h/2 {
		t.write(scrollUp)
		t.f.Move(1)
		t.drawLine(t.current - 1)
		t.drawLine(t.current)
		t.drawLine(t.h - 2)
	} else if t.current < t.h-2 {
		curr := t.current
		t.current++
		t.drawLine(curr)
		t.drawLine(t.current)
	}
}
func (t *term) pgUp() {
	t.f.Move(-t.h + 2)
	if t.f.Position() < 0 {
		t.home()
		return
	}
	t.redraw()
}
func (t *term) pgDn() {
	t.f.Move(t.h - 2)
	if t.f.Position()+t.h-2 > t.f.LinesCount() {
		t.end()
		return
	}
	t.redraw()
}
func (t *term) home() {
	t.f.SetPosition(0)
	t.current = 0
	t.redraw()
}
func (t *term) end() {
	t.f.SetPosition(t.f.LinesCount() - t.h + 1)
	t.current = t.h - 2
	t.redraw()
}

func (t *term) execute() {
	if t.command == "" {
		return
	}
	if t.command == ":" {
		t.command = ""
		return
	}
	c := t.findCommand()
	if c != nil {
		c.execFn(t)
		t.command = ""
		return
	}
	t.message = fmt.Sprintf("%s: undefined command", t.command)
	t.command = ""
}

func (t *term) findCommand() *command {
	for cl, c := range t.commands {
		if len(t.command) >= len(cl) && cl == t.command[:len(cl)] {
			return c
		}
	}
	return nil
}

func (t *term) showCurrent() {
	t.clear()
	m := t.f.Line(t.current)
	i := 1
	for k, v := range m {
		t.goTo(i, 1)
		mess := fmt.Sprintf("%s%s%s:\t%v", bold, k, reset, v)
		t.writeFull(mess)
		i += len(mess)/t.w + 1
	}
	t.message = "Press ENTER to continue"
}

func (t *term) search(changeDir bool) {
	if t.lastSearch.mask == "" {
		if t.lastSearch.idx == -1 {
			t.message = "nothing to search"
		}
		return
	}

	dir := t.lastSearch.dir
	if changeDir {
		dir = 1 - dir
	}
	var idx int
	var err error
	if t.lastSearch.tag != "" {
		idx, err = t.f.SearchTag(t.lastSearch.tag, t.lastSearch.mask, t.lastSearch.idx, dir, t.lastSearch.isRegexp)
	} else {
		idx, err = t.f.Search(t.lastSearch.mask, t.lastSearch.idx, dir, t.lastSearch.isRegexp)
	}
	if err != nil {
		t.message = err.Error()
		return
	}
	if idx == -1 {
		t.message = "not found"
		return
	}
	if dir == SearchForward {
		t.lastSearch.idx = idx + 1
	} else {
		t.lastSearch.idx = idx - 1
	}
	hh := t.h / 2
	if idx-hh < 0 {
		hh = idx
	}
	t.f.SetPosition(idx - hh)
	t.current = hh
	t.redraw()
}

func (t *term) write(s string) error {
	b := []byte(s)
	if len(b) > t.w {
		b = b[:t.w]
	}
	return t.writeFull(string(b))
}

func (t *term) writeFull(s string) error {
	_, err := t.t.Write([]byte(s))
	return err
}

func (t *term) clear() error {
	return t.write("\033[2J")
}

func (t *term) clearLine(beh ...int) error {
	arg := 2
	if len(beh) > 0 {
		arg = beh[0]
	}
	return t.write(fmt.Sprintf("\033[%dK", arg))
}

func (t *term) goTo(row, col int) error {
	return t.write(fmt.Sprintf("\033[%d;%dH", row, col))
}

func (t *term) setColor(fg, bg int) error {
	if fg == 0 {
		fg = fgDefault
	}
	if bg == 0 {
		bg = bgDefault
	}
	return t.write(fmt.Sprintf("\033[%d;%dm", fg, bg))
}

func (t *term) resetColor() error {
	return t.setColor(0, 0)
}

func newOptions(opts ...string) *options {
	opt := &options{}
	l := len(opts) / 2
	if l > 0 {
		opt.options = make([]option, l)
		i := 0
		for i+1 < len(opts) {
			opt.options[i/2] = option{name: opts[i], command: opts[i+1]}
			i += 2
		}
	}
	return opt
}

func newOptionsFromArray(opts []string, appendSlash bool) *options {
	ret := &options{options: make([]option, len(opts))}
	for i, opt := range opts {
		cmd := opt
		if appendSlash {
			cmd += "/"
		}
		ret.options[i] = option{name: opt, command: cmd}
	}
	return ret
}

func (o *options) add(name, command string) *options {
	o.options = append(o.options, option{name: name, command: command})
	return o
}

func (o *options) next() {
	if len(o.options) == 0 {
		return
	}
	curr := o.current + 1

	for curr != o.current {
		if curr >= len(o.options) {
			curr = 0
		}
		if o.options[curr].visible {
			o.current = curr
			return
		}
		curr++
	}
}

func (o *options) prev() {
	if len(o.options) == 0 {
		return
	}
	curr := o.current - 1

	for curr != o.current {
		if curr < 0 {
			curr = len(o.options) - 1
		}
		if o.options[curr].visible {
			o.current = curr
			return
		}
		curr--
	}
}
func (t *term) fillCommands() {

	t.commands[":f"] = &command{
		name:      fmt.Sprintf(templBoldSuff, "f", "ilter"),
		optionsFn: filterCommandOptions,
		execFn:    filterCommandExecute,
	}
	t.commands[":s"] = &command{
		name:      fmt.Sprintf(templBoldSuff, "s", "earch-tag"),
		optionsFn: searchCommandOptions,
		execFn:    searchCommandExecute,
	}
	t.commands[":p"] = &command{
		name:   "",
		execFn: func(t *term) { t.message = fmt.Sprintf("%d", os.Getpid()) },
	}
	t.commands[":x"] = &command{
		name:   fmt.Sprintf(templBoldFull, "e", "x", "it"),
		execFn: func(t *term) { t.exit = true },
	}
	t.commands[":q"] = &command{
		name:   fmt.Sprintf(templBoldSuff, "q", "uit"),
		execFn: func(t *term) { t.exit = true },
	}

	t.commands["/"] = &command{
		name:   "search(/)",
		execFn: simpleSearchExecute,
	}
	t.commands["?"] = &command{
		name:   "search-up(?)",
		execFn: simpleSearchExecute,
	}
}
func filterCommandExecute(t *term) {
	if t.command == ":fu" {
		t.f = t.f.Up()
	} else if t.command == ":fr" {
		t.f = t.f.Top()
	} else {
		r := regexp.MustCompile("^f\\/([a-zA-Z]+)\\/([^\\/]*)(\\/([+!\\$-])?)?$")
		comm := r.FindStringSubmatch(t.command[1:])
		if comm != nil {
			op := FOEqual
			if len(comm) == 5 && comm[4] != "" {
				switch comm[4] {
				case "+":
					op = FOGreaterOrEqual
				case "-":
					op = FOLessOrEqual
				case "!":
					op = FONotEqual
				case "$":
					op = FORegexp
				}
			}
			t.f = t.f.Filter(Filter{Mask: comm[2], Operator: op, Tag: comm[1]})
		}
	}
	t.redraw()
}
func simpleSearchExecute(t *term) {
	t.lastSearch = searchParams{mask: t.command[1:], idx: t.f.Position() + t.current, isRegexp: false, tag: ""}
	if t.command[:1] == "/" {
		t.lastSearch.dir = SearchForward
	} else {
		t.lastSearch.dir = SearchBack
	}
	t.search(false)
}

func searchCommandExecute(t *term) {
	t.lastSearch = searchParams{idx: t.f.Position() + t.current, isRegexp: false, tag: ""}
	r := regexp.MustCompile("^s\\/([a-zA-Z]+)\\/([^\\/]*)(\\/(\\$))?$")
	comm := r.FindStringSubmatch(t.command[1:])
	if comm != nil {
		if len(comm) == 5 && comm[4] != "" {
			switch comm[4] {
			case "$":
				t.lastSearch.isRegexp = true
			}
		}
		t.lastSearch.mask = comm[2]
		t.lastSearch.tag = comm[1]
		t.search(false)
	}
}
func filterCommandOptions(t *term) {
	r := regexp.MustCompile("^:f\\/([a-zA-Z0-9]*)?(\\/([a-zA-Z0-9]*))?$")
	comm := r.FindStringSubmatch(t.command)
	if comm != nil {
		if len(comm) > 2 && comm[2] != "" {
			t.options = newOptionsFromArray(t.f.Levels(), true)
			t.options.prefix = comm[3]
			t.command = ":f/" + comm[1] + "/"
			return
		}
		t.options = newOptionsFromArray(t.f.KnownTags(), true)
		t.options.prefix = comm[1]
		t.command = ":f/"
		return
	}
	t.command = ":f/"
}
func searchCommandOptions(t *term) {
	if t.command == ":s/" {
		t.options = newOptionsFromArray(t.f.KnownTags(), true)
	} else if t.command == ":s" {
		t.options = newOptions(
			"/", "/",
		)
	}
}
