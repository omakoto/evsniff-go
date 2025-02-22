package evutil

import (
	"github.com/holoplot/go-evdev"
	"github.com/omakoto/go-common/src/must"
	"regexp"
)

func ptr(value bool) *bool {
	return &value
}

var ptrue = ptr(true)
var pfalse = ptr(false)

func Matches(sel Selector, idev *evdev.InputDevice) bool {
	b := sel.Matches(idev)
	if b == nil {
		return true
	}
	return *b
}

type Selector interface {
	Matches(idev *evdev.InputDevice) *bool
}
type constantSelector struct {
	b bool
}

var _ = Selector((*constantSelector)(nil))

func NewAllSelector() Selector {
	return &constantSelector{true}
}

func NewNoneSelector() Selector {
	return &constantSelector{true}
}

func (s *constantSelector) Matches(idev *evdev.InputDevice) *bool {
	return &s.b
}

type NegativeSelector struct {
	selector Selector
}

var _ = Selector((*NegativeSelector)(nil))

func NewNegativeSelector(selector Selector) *NegativeSelector {
	return &NegativeSelector{selector}
}

func (s *NegativeSelector) Matches(idev *evdev.InputDevice) *bool {
	b := s.selector.Matches(idev)
	if b == nil {
		return nil // unknown -> unknown
	}
	if *b {
		return pfalse // true -> definitely false
	}
	return nil // false -> still unknown
}

type OrSelector struct {
	selectors []Selector
}

var _ = Selector((*OrSelector)(nil))

func NewOrSelector() *OrSelector {
	return &OrSelector{}
}

func (s *OrSelector) Add(sel Selector) *OrSelector {
	s.selectors = append(s.selectors, sel)
	return s
}

func (s *OrSelector) IsEmpty() bool {
	return len(s.selectors) == 0
}

func (s *OrSelector) Matches(idev *evdev.InputDevice) *bool {
	if len(s.selectors) == 0 {
		return nil
	}
	for _, sel := range s.selectors {
		b := sel.Matches(idev)
		if b != nil {
			return b
		}
	}
	return pfalse
}

type ReSelector struct {
	regex *regexp.Regexp
}

var _ = Selector((*ReSelector)(nil))

func NewReSelector(pattern string) *ReSelector {
	return &ReSelector{regex: regexp.MustCompile("(?i)" + pattern)}
}

func (s *ReSelector) Matches(idev *evdev.InputDevice) *bool {
	if s.regex.MatchString(must.Must2(idev.Name())) {
		return ptrue
	}
	return nil
}

type PathSelector struct {
	path string
}

var _ = Selector((*PathSelector)(nil))

func NewPathSelector(path string) *PathSelector {
	return &PathSelector{path}
}

func (s *PathSelector) Matches(idev *evdev.InputDevice) *bool {
	if idev.Path() == s.path {
		return ptrue
	}
	return nil
}
