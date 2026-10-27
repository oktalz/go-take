package main

import (
	"cmp"
	"errors"
	"flag"
	"os"
	"time"

	"github.com/oktalz/go-take/cast"
)

// timeFlag is a flag holding a time in seconds, accepting "1:02.5" notation
// or the label of a marker, which resolveTimes looks up once the recording is loaded.
type timeFlag struct {
	marker string
	v      float64
	set    bool
}

func (t *timeFlag) String() string {
	if t.marker != "" {
		return t.marker
	}
	return fmtTime(t.v)
}

func (t *timeFlag) Set(s string) error {
	v, err := parseTime(s)
	t.v, t.set, t.marker = v, true, ""
	if err != nil {
		t.marker = s
	}
	return nil
}

// resolveTimes turns marker labels given to time flags into times in c.
func resolveTimes(c *cast.Cast, flags ...*timeFlag) error {
	for _, t := range flags {
		if t.marker == "" {
			continue
		}
		v, err := c.MarkerTime(t.marker)
		if err != nil {
			return err
		}
		t.v = v
	}
	return nil
}

// editFlags holds the flags shared by all CLI edit commands.
type editFlags struct {
	find       string
	replace    string
	out        string
	label      string
	prompt     string
	command    string
	output     string
	outputFile string
	after      string
	pattern    string
	text       string
	from, to   timeFlag
	at         timeFlag
	factor     float64
	maxIdle    float64
	delay      float64
	jitter     float64
	seconds    float64
	regex      bool
	ignoreCase bool
	raw        bool
}

// span returns the event index range selected by -from and -to.
func (f *editFlags) span(c *cast.Cast) (from, to int) {
	from, to = 0, len(c.Events)
	if f.from.set {
		from = c.IndexAt(f.from.v)
	}
	if f.to.set {
		to = c.IndexAt(f.to.v)
	}
	return from, to
}

func seed() uint64 { return uint64(time.Now().UnixNano()) }

type editCmd struct {
	run      func(c *cast.Cast, f *editFlags) error
	position string // "range" adds -from/-to, "at" adds -at
}

var editCmds = map[string]editCmd{
	"cut": {position: "range", run: func(c *cast.Cast, f *editFlags) error {
		if !f.from.set && !f.to.set {
			return errors.New("cut: need -from and/or -to")
		}
		return c.Delete(f.span(c))
	}},
	"stretch": {position: "range", run: func(c *cast.Cast, f *editFlags) error {
		if f.factor <= 0 {
			return errors.New("stretch: -factor must be > 0")
		}
		from, to := f.span(c)
		return c.Stretch(from, to, f.factor)
	}},
	"speed": {run: func(c *cast.Cast, f *editFlags) error {
		if f.factor <= 0 {
			return errors.New("speed: -factor must be > 0")
		}
		return c.Stretch(0, len(c.Events), 1/f.factor)
	}},
	"idle": {run: func(c *cast.Cast, f *editFlags) error {
		return c.CapIdle(0, len(c.Events), f.maxIdle)
	}},
	"typing": {position: "range", run: func(c *cast.Cast, f *editFlags) error {
		from, to := f.span(c)
		n, err := c.NormalizeTyping(from, to, cast.TypingOptions{Delay: f.delay, Jitter: f.jitter, Seed: seed()})
		eprintf("adjusted %d keystrokes\n", n)
		return err
	}},
	"inject": {position: "at", run: runInject},
	"replace": {position: "range", run: func(c *cast.Cast, f *editFlags) error {
		if f.find == "" {
			return errors.New("replace: need -find")
		}
		from, to := f.span(c)
		m, ev, err := c.Replace(from, to, cast.ReplaceOptions{
			Find: unescape(f.find), Replace: unescape(f.replace),
			Regex: f.regex, IgnoreCase: f.ignoreCase, IgnoreANSI: !f.raw,
		})
		eprintf("replaced %d matches in %d events\n", m, ev)
		return err
	}},
	"caption": {position: "at", run: func(c *cast.Cast, f *editFlags) error {
		c.Insert(c.IndexAt(f.at.v), cast.Event{Code: cast.Caption, Data: unescape(f.text)})
		return nil
	}},
	"redact": {position: "range", run: func(c *cast.Cast, f *editFlags) error {
		from, to := f.span(c)
		m, ev, err := c.Replace(from, to, cast.ReplaceOptions{
			Find: cmp.Or(f.pattern, cast.SecretPattern), Regex: true, Mask: true,
			IgnoreCase: f.ignoreCase, IgnoreANSI: !f.raw,
		})
		eprintf("masked %d matches in %d events\n", m, ev)
		return err
	}},
	"pause": {position: "at", run: func(c *cast.Cast, f *editFlags) error {
		c.Pause(c.IndexAt(f.at.v), f.seconds)
		return nil
	}},
	"trim-exit": {run: func(c *cast.Cast, _ *editFlags) error {
		if n := c.TrimExit(); n > 0 {
			eprintf("removed the trailing exit (%d events)\n", n)
		} else {
			eprintf("no trailing exit found\n")
		}
		return nil
	}},
	"marker": {position: "at", run: func(c *cast.Cast, f *editFlags) error {
		c.Insert(c.IndexAt(f.at.v), cast.Event{Code: cast.Marker, Data: f.label})
		return nil
	}},
}

func runInject(c *cast.Cast, f *editFlags) error {
	if f.command == "" && f.output == "" && f.outputFile == "" {
		return errors.New("inject: need -cmd and/or -output")
	}
	text := f.output
	if f.outputFile != "" {
		b, err := os.ReadFile(f.outputFile)
		if err != nil {
			return err
		}
		text = string(b)
	}
	o := cast.DefaultInjectOptions()
	o.Prompt, o.Command, o.Output, o.After = unescape(f.prompt), f.command, unescape(text), unescape(f.after)
	o.TypingDelay, o.Jitter, o.Seed = f.delay, f.jitter, seed()
	n := c.InjectCommand(c.IndexAt(f.at.v), o)
	eprintf("inserted %d events\n", n)
	return nil
}

// cmdEditOp runs one of the CLI edit commands against FILE.
func cmdEditOp(name string, args []string) error {
	ec, ok := editCmds[name]
	if !ok {
		return errors.New("unknown edit command " + name)
	}
	var f editFlags
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&f.out, "o", "", "write result to this file instead of modifying FILE")
	switch ec.position {
	case "range":
		fs.Var(&f.from, "from", "section start: time or marker (default: beginning)")
		fs.Var(&f.to, "to", "section end: time or marker (default: end)")
	case "at":
		fs.Var(&f.at, "at", "position to insert at: time or marker")
	default:
	}
	fs.Float64Var(&f.factor, "factor", 0, "time factor (stretch: >1 slower; speed: >1 faster)")
	fs.Float64Var(&f.maxIdle, "max", 1, "maximum pause in seconds")
	fs.Float64Var(&f.delay, "delay", 0.08, "delay between keystrokes in seconds")
	fs.Float64Var(&f.jitter, "jitter", 0.03, "random variation of keystroke delay")
	fs.Float64Var(&f.seconds, "seconds", 1, "pause length in seconds")
	fs.StringVar(&f.label, "label", "", "marker label")
	fs.StringVar(&f.text, "text", "", "caption text (\\n line breaks; empty hides the caption)")
	fs.StringVar(&f.prompt, "prompt", "$ ", "prompt printed before the command")
	fs.StringVar(&f.command, "cmd", "", "command to type")
	fs.StringVar(&f.output, "output", "", "command output (\\n line breaks)")
	fs.StringVar(&f.outputFile, "output-file", "", "read command output from this file")
	fs.StringVar(&f.find, "find", "", "text (or -regex) to find in output")
	fs.StringVar(&f.replace, "replace", "", "replacement text ($1 with -regex)")
	fs.BoolVar(&f.regex, "regex", false, "-find is a regular expression")
	fs.StringVar(&f.pattern, "pattern", "",
		"redact: regular expression to mask; (?P<secret>...) masks only that part "+
			"(default: common tokens and passwords)")
	fs.BoolVar(&f.ignoreCase, "ignore-case", false, "case-insensitive -find")
	fs.BoolVar(&f.raw, "raw", false, "match raw data including escape sequences (default: visible text only)")
	fs.StringVar(&f.after, "after", "", "text printed after the output (e.g. next prompt)")
	path, err := parseFile(fs, args)
	if err != nil {
		return err
	}
	c, err := cast.Load(path)
	if err != nil {
		return err
	}
	if err := resolveTimes(c, &f.from, &f.to, &f.at); err != nil {
		return err
	}
	before := c.Duration()
	if err := ec.run(c, &f); err != nil {
		return err
	}
	dst := path
	if f.out != "" {
		dst = f.out
	}
	if err := c.Save(dst); err != nil {
		return err
	}
	eprintf("%s: %s -> %s, %d events\n", dst, fmtTime(before), fmtTime(c.Duration()), len(c.Events))
	return nil
}

// escapes maps the escape letters accepted on the command line to their bytes.
var escapes = map[byte]byte{'n': '\n', 'r': '\r', 't': '\t', 'e': 0x1b, '\\': '\\'}

// unescape interprets \n, \r, \t, \e and \\ in command line text.
func unescape(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			if r, ok := escapes[s[i+1]]; ok {
				b = append(b, r)
				i++
				continue
			}
		}
		b = append(b, s[i])
	}
	return string(b)
}
