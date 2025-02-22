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

	// IsPositive means this selector will increase selection
	// Otherwise, it will remove already selected elements.
	IsPositive() bool
}
type constantSelector struct {
	b bool
}

var _ = Selector((*constantSelector)(nil))

func NewAllSelector() Selector {
	return &constantSelector{true}
}

func NewNoneSelector() Selector {
	return &constantSelector{false}
}

func (s *constantSelector) IsPositive() bool {
	return s.b
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

func (s *NegativeSelector) IsPositive() bool {
	return !s.selector.IsPositive()
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

type CombinedSelector struct {
	selectors []Selector
	def       bool
}

var _ = Selector((*CombinedSelector)(nil))

func NewCombinedSelector() *CombinedSelector {
	return &CombinedSelector{def: true}
}

func (s *CombinedSelector) IsPositive() bool {
	return true
}

func (s *CombinedSelector) Add(sel Selector) *CombinedSelector {
	s.selectors = append(s.selectors, sel)

	// Once we add any positive filter, we should default to false.
	if sel.IsPositive() {
		s.def = false
	}
	return s
}

func (s *CombinedSelector) IsEmpty() bool {
	return len(s.selectors) == 0
}

func (s *CombinedSelector) Matches(idev *evdev.InputDevice) *bool {
	positiveMatched := false
	negativeMatched := false
	for _, sel := range s.selectors {
		b := sel.Matches(idev)
		if b == nil {
			continue // Ignore unknowns
		}
		if sel.IsPositive() {
			if *b {
				positiveMatched = true
			}
		} else {
			if !*b {
				negativeMatched = true
			}
		}
	}
	// If there's any positive match, return true unless there's a negative match.
	if positiveMatched {
		return ptr(!negativeMatched)
	}
	// If there's any negative match, (and there's no positive match), return false.
	if negativeMatched {
		return pfalse
	}

	return ptr(s.def)
}

type ReSelector struct {
	regex *regexp.Regexp
}

var _ = Selector((*ReSelector)(nil))

func NewReSelector(pattern string) *ReSelector {
	return &ReSelector{regex: regexp.MustCompile("(?i)" + pattern)}
}

func (s *ReSelector) IsPositive() bool {
	return true
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

func (s *PathSelector) IsPositive() bool {
	return true
}

func (s *PathSelector) Matches(idev *evdev.InputDevice) *bool {
	if idev.Path() == s.path {
		return ptrue
	}
	return nil
}
