// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// MediaJSON is the media type typed endpoints consume and produce by default.
const MediaJSON = "application/json"

// codec decodes request bodies and encodes response bodies of one format.
type codec interface {
	decode(body []byte, v any) error
	encode(w io.Writer, v any) error
}

// codecs are the supported media types of Consumes() and Produces(). Fixed by
// schemaf; see EXTEND.md, "Content types".
var codecs = map[string]codec{
	MediaJSON:            jsonCodec{},
	"application/yaml":   yamlCodec{},
	"application/x-yaml": yamlCodec{},
}

// SupportedMediaTypes returns the media types Consumes() and Produces() may
// list, sorted.
func SupportedMediaTypes() []string {
	out := make([]string, 0, len(codecs))
	for m := range codecs {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// CheckMediaTypes validates a Consumes() or Produces() list: non-empty, no
// duplicates, supported media types only. method names the list in errors.
func CheckMediaTypes(method string, types []string) error {
	if len(types) == 0 {
		return fmt.Errorf("%s() returns no media types", method)
	}
	seen := map[string]bool{}
	for _, t := range types {
		if _, ok := codecs[t]; !ok {
			return fmt.Errorf("%s(): unsupported media type %q (supported: %s)", method, t, strings.Join(SupportedMediaTypes(), ", "))
		}
		if seen[t] {
			return fmt.Errorf("%s(): media type %q listed twice", method, t)
		}
		seen[t] = true
	}
	return nil
}

type jsonCodec struct{}

func (jsonCodec) decode(body []byte, v any) error { return json.Unmarshal(body, v) }
func (jsonCodec) encode(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }

// yamlCodec reads and writes YAML with the JSON field names: a YAML body is
// the JSON body written as YAML. It goes through JSON both ways so the json
// tags (and json.Marshaler implementations) apply.
type yamlCodec struct{}

func (yamlCodec) decode(body []byte, v any) error {
	var doc any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	j, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("yaml: %w (mapping keys must be strings)", err)
	}
	return json.Unmarshal(j, v)
}

func (yamlCodec) encode(w io.Writer, v any) error {
	j, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// JSON is YAML: parse it into a node tree (keeps the field order), then
	// drop the JSON flow and quoting styles so it is written as block YAML.
	var node yaml.Node
	if err := yaml.Unmarshal(j, &node); err != nil {
		return err
	}
	clearStyle(&node)
	enc := yaml.NewEncoder(w)
	if err := enc.Encode(&node); err != nil {
		return err
	}
	return enc.Close()
}

func clearStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		clearStyle(c)
	}
}

// mediaTypes reads an endpoint's optional Consumes() or Produces(); the default
// is JSON. Invalid lists panic: codegen rejects them before they get here.
func mediaTypes(e any, method string) []string {
	var types []string
	switch method {
	case "Consumes":
		c, ok := e.(interface{ Consumes() []string })
		if !ok {
			return []string{MediaJSON}
		}
		types = c.Consumes()
	case "Produces":
		p, ok := e.(interface{ Produces() []string })
		if !ok {
			return []string{MediaJSON}
		}
		types = p.Produces()
	}
	if err := CheckMediaTypes(method, types); err != nil {
		panic(fmt.Sprintf("api: %T: %v", e, err))
	}
	return types
}

var errUnsupportedMediaType = errors.New("unsupported media type")

// requestCodec picks the codec for a request body by its Content-Type; no
// Content-Type means the first entry of consumes.
func requestCodec(contentType string, consumes []string) (codec, error) {
	if contentType == "" {
		return codecs[consumes[0]], nil
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, errUnsupportedMediaType
	}
	for _, c := range consumes {
		if c == mt {
			return codecs[c], nil
		}
	}
	return nil, errUnsupportedMediaType
}

// negotiate picks the entry of produces to answer with for an Accept header:
// the highest q, ties going to the order of produces. Each entry takes the q
// of the most specific matching range. ok is false if nothing is acceptable.
func negotiate(accept string, produces []string) (string, bool) {
	if strings.TrimSpace(accept) == "" {
		return produces[0], true
	}
	type rng struct {
		typ, sub string
		q        float64
	}
	var ranges []rng
	// Ranges that don't parse (or carry an invalid q) are ignored, as
	// RFC 9110 lets a server do; an Accept with no valid range is absent.
	for _, part := range strings.Split(accept, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || strings.HasPrefix(part, "*;") { // legacy bare wildcard
			part = "*/*" + part[1:]
		}
		mt, params, err := mime.ParseMediaType(part)
		if err != nil {
			continue
		}
		typ, sub, ok := strings.Cut(mt, "/")
		if !ok {
			continue
		}
		q := 1.0
		if qs, ok := params["q"]; ok {
			f, err := strconv.ParseFloat(qs, 64)
			if err != nil || f < 0 || f > 1 {
				continue
			}
			q = f
		}
		ranges = append(ranges, rng{typ, sub, q})
	}
	if len(ranges) == 0 {
		return produces[0], true
	}
	best, bestQ := "", 0.0
	for _, p := range produces {
		typ, sub, _ := strings.Cut(p, "/")
		q, spec := 0.0, -1
		for _, r := range ranges {
			s := -1
			switch {
			case r.typ == typ && r.sub == sub:
				s = 2
			case r.typ == typ && r.sub == "*":
				s = 1
			case r.typ == "*" && r.sub == "*":
				s = 0
			}
			if s > spec {
				spec, q = s, r.q
			}
		}
		if q > bestQ {
			best, bestQ = p, q
		}
	}
	return best, bestQ > 0
}
