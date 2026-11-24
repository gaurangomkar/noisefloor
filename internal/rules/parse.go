// Package rules reads Prometheus alerting rule files and matches a firing
// alert back to the rule that produced it.
package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gaurangomkar/noisefloor/internal/duration"
	"github.com/gaurangomkar/noisefloor/internal/model"
)

// Rule is one alerting rule. Recording rules are not indexed.
type Rule struct {
	File          string
	Group         string
	Line          int
	Alert         string
	Expr          string
	For           time.Duration
	KeepFiringFor time.Duration
	Labels        map[string]string
	Annotations   map[string]string
}

// Match is the view of a rule stored on a report.
func (r Rule) Match() *model.RuleMatch {
	m := &model.RuleMatch{
		File:        r.File,
		Group:       r.Group,
		Line:        r.Line,
		Expr:        strings.TrimSpace(r.Expr),
		Labels:      r.Labels,
		Annotations: r.Annotations,
	}
	if r.For > 0 {
		m.For = r.For.String()
	}
	if r.KeepFiringFor > 0 {
		m.KeepFiringFor = r.KeepFiringFor.String()
	}
	return m
}

// Index looks up alerting rules by alert name.
type Index struct {
	byName map[string][]Rule
	files  int
}

// Empty is an index with no rules.
func Empty() *Index {
	return &Index{byName: map[string][]Rule{}}
}

// Load reads every file matched by paths. Paths are relative to baseDir when
// they are not absolute. A directory is walked for .yml and .yaml files.
// Display paths in the index are relative to baseDir when possible.
func Load(baseDir string, paths []string) (*Index, error) {
	if len(paths) == 0 {
		return Empty(), nil
	}
	files, err := expand(baseDir, paths)
	if err != nil {
		return nil, err
	}
	idx := Empty()
	idx.files = len(files)
	for _, f := range files {
		rules, err := parseFile(f.abs, f.display)
		if err != nil {
			return nil, err
		}
		for _, r := range rules {
			idx.byName[r.Alert] = append(idx.byName[r.Alert], r)
		}
	}
	for name, list := range idx.byName {
		sort.Slice(list, func(i, j int) bool {
			if list[i].File != list[j].File {
				return list[i].File < list[j].File
			}
			if list[i].Line != list[j].Line {
				return list[i].Line < list[j].Line
			}
			return list[i].Group < list[j].Group
		})
		idx.byName[name] = list
	}
	return idx, nil
}

// Count is the number of alerting rules.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	n := 0
	for _, list := range idx.byName {
		n += len(list)
	}
	return n
}

// Files is the number of files that were opened.
func (idx *Index) Files() int {
	if idx == nil {
		return 0
	}
	return idx.files
}

// Lookup returns the alerting rule whose labels best match the firing alert.
// Rules that share the alert name are ranked by how many of their labels are
// present and equal on the event. The first rule in file order wins a tie.
func (idx *Index) Lookup(alertname string, labels map[string]string) (Rule, bool) {
	if idx == nil {
		return Rule{}, false
	}
	cands := idx.byName[alertname]
	if len(cands) == 0 {
		return Rule{}, false
	}
	best := cands[0]
	bestScore := matchScore(best, labels)
	for _, c := range cands[1:] {
		s := matchScore(c, labels)
		if s > bestScore {
			best = c
			bestScore = s
		}
	}
	return best, true
}

func matchScore(r Rule, labels map[string]string) int {
	score := 0
	for k, v := range r.Labels {
		if labels[k] == v {
			score += 2
		} else {
			score--
		}
	}
	return score
}

type fileRef struct {
	abs     string
	display string
}

func expand(base string, paths []string) ([]fileRef, error) {
	var out []fileRef
	seen := map[string]bool{}
	for _, p := range paths {
		absPattern := p
		if !filepath.IsAbs(p) {
			absPattern = filepath.Join(base, p)
		}
		matches, err := filepath.Glob(absPattern)
		if err != nil {
			return nil, fmt.Errorf("rules path %q: %w", p, err)
		}
		if len(matches) == 0 {
			// A path with no metacharacters that does not exist also yields no matches.
			if _, err := os.Stat(absPattern); err != nil {
				return nil, fmt.Errorf("rules path %q matched no files", p)
			}
			matches = []string{absPattern}
		}
		for _, m := range matches {
			files, err := filesFrom(p, m)
			if err != nil {
				return nil, err
			}
			for _, abs := range files {
				if seen[abs] {
					continue
				}
				seen[abs] = true
				out = append(out, fileRef{abs: abs, display: displayPath(base, abs)})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].display < out[j].display })
	if len(out) == 0 {
		return nil, fmt.Errorf("no rule files found")
	}
	return out, nil
}

func filesFrom(pattern, match string) ([]string, error) {
	info, err := os.Stat(match)
	if err != nil {
		return nil, fmt.Errorf("rules path %q: %w", pattern, err)
	}
	if !info.IsDir() {
		return []string{match}, nil
	}
	found, err := walkYAML(match)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("rules path %q contains no yaml files", pattern)
	}
	return found, nil
}

func walkYAML(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yml", ".yaml":
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func displayPath(base, abs string) string {
	if base == "" {
		return filepath.ToSlash(abs)
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func parseFile(abs, display string) ([]Rule, error) {
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read rules %s: %w", display, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", display, err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("%s: empty document", display)
	}
	doc := root.Content[0]
	groups := mappingValue(doc, "groups")
	if groups == nil {
		return nil, fmt.Errorf("%s: missing groups", display)
	}
	if groups.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s:%d: groups must be a list", display, groups.Line)
	}
	var rules []Rule
	for _, g := range groups.Content {
		nameNode := mappingValue(g, "name")
		groupName := ""
		if nameNode != nil {
			groupName = nameNode.Value
		}
		list := mappingValue(g, "rules")
		if list == nil {
			continue
		}
		if list.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("%s:%d: rules must be a list", display, list.Line)
		}
		for _, item := range list.Content {
			rule, skip, err := parseRule(display, groupName, item)
			if err != nil {
				return nil, err
			}
			if skip {
				continue
			}
			rules = append(rules, rule)
		}
	}
	return rules, nil
}

func parseRule(file, group string, node *yaml.Node) (Rule, bool, error) {
	alert := scalar(node, "alert")
	record := scalar(node, "record")
	if alert != "" && record != "" {
		return Rule{}, false, fmt.Errorf("%s:%d: rule has both alert and record", file, node.Line)
	}
	if alert == "" && record == "" {
		return Rule{}, false, fmt.Errorf("%s:%d: rule has neither alert nor record", file, node.Line)
	}
	if record != "" {
		return Rule{}, true, nil
	}
	expr := scalar(node, "expr")
	forRaw := scalar(node, "for")
	keepRaw := scalar(node, "keep_firing_for")
	forDur, err := optDuration(forRaw)
	if err != nil {
		return Rule{}, false, fmt.Errorf("%s:%d: for: %w", file, node.Line, err)
	}
	keepDur, err := optDuration(keepRaw)
	if err != nil {
		return Rule{}, false, fmt.Errorf("%s:%d: keep_firing_for: %w", file, node.Line, err)
	}
	line := node.Line
	if key := mappingKey(node, "alert"); key != nil && key.Line > 0 {
		line = key.Line
	}
	return Rule{
		File:          file,
		Group:         group,
		Line:          line,
		Alert:         alert,
		Expr:          expr,
		For:           forDur,
		KeepFiringFor: keepDur,
		Labels:        stringMap(mappingValue(node, "labels")),
		Annotations:   stringMap(mappingValue(node, "annotations")),
	}, false, nil
}

func optDuration(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	return duration.Parse(s)
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func mappingKey(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i]
		}
	}
	return nil
}

func scalar(node *yaml.Node, key string) string {
	v := mappingValue(node, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

func stringMap(node *yaml.Node) map[string]string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	out := make(map[string]string, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]
		if k.Kind != yaml.ScalarNode || v.Kind != yaml.ScalarNode {
			continue
		}
		out[k.Value] = v.Value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
