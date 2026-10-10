package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var placeholder = regexp.MustCompile(`\{[a-z.]+\}`)

// listFlag is a flag that may be given more than once; each value may hold
// several, separated by commas.
type listFlag struct{ values []string }

func (l *listFlag) String() string { return strings.Join(l.values, ",") }
func (l *listFlag) Set(v string) error {
	l.values = append(l.values, v)
	return nil
}

// flags is the flag set of one command with where each value goes.
type flags struct {
	fs       *flag.FlagSet
	g        globals
	strs     map[string]*string
	ints     map[string]*int64
	floats   map[string]*float64
	bools    map[string]*bool
	lists    map[string]*listFlag
	fileFlag map[string]*listFlag
	page     *int
	pageSize *int
	dataFile *string
	output   *string
	wait     *bool
	follow   *bool
	timeout  *string
}

func newFlags(cmd command, env Env, base globals) *flags {
	f := &flags{
		fs: flag.NewFlagSet(cmd.Resource+" "+cmd.Verb, flag.ContinueOnError), g: base,
		strs: map[string]*string{}, ints: map[string]*int64{}, floats: map[string]*float64{}, bools: map[string]*bool{},
		lists: map[string]*listFlag{}, fileFlag: map[string]*listFlag{},
	}
	f.fs.SetOutput(io.Discard)
	f.g.register(f.fs, env)
	f.g.server, f.g.token, f.g.json = base.server, base.token, base.json
	f.fs.Lookup("server").DefValue = base.server
	for _, fl := range append(append([]field(nil), cmd.Query...), cmd.Body...) {
		switch fl.Kind {
		case kString, kJSON, kText:
			f.strs[fl.Flag] = f.fs.String(fl.Flag, "", fl.Help)
		case kInt:
			f.ints[fl.Flag] = f.fs.Int64(fl.Flag, 0, fl.Help)
		case kFloat:
			f.floats[fl.Flag] = f.fs.Float64(fl.Flag, 0, fl.Help)
		case kBool:
			f.bools[fl.Flag] = f.fs.Bool(fl.Flag, false, fl.Help)
		case kList:
			l := &listFlag{}
			f.lists[fl.Flag] = l
			f.fs.Var(l, fl.Flag, fl.Help)
		}
	}
	for _, fp := range cmd.Files {
		l := &listFlag{}
		f.fileFlag[fp.Flag] = l
		f.fs.Var(l, fp.Flag, fp.Help)
	}
	if cmd.Paged {
		f.page = f.fs.Int("page", 0, "the page, from 1")
		f.pageSize = f.fs.Int("page-size", 0, "items on a page (at most 200)")
	}
	if takesBody(cmd) {
		f.dataFile = f.fs.String("file", "", "read the JSON body from this file (- for standard input); flags override it")
	}
	if cmd.Result == resultBytes {
		f.output = f.fs.String("output", "", "where to save the file (- for standard output)")
	}
	switch cmd.Special {
	case "wait":
		f.wait = f.fs.Bool("wait", false, "wait until the command is finished and say how it goes")
		f.timeout = f.fs.String("timeout", "", "with --wait: give up after this long, such as 30m")
	case "follow":
		f.follow = f.fs.Bool("follow", false, "keep watching and print events as they happen")
	}
	return f
}

// parse reads the flags and the positional arguments in any order.
func (f *flags) parse(args []string) ([]string, error) {
	var pos []string
	for len(args) > 0 {
		if err := f.fs.Parse(args); err != nil {
			return nil, usagef("%v", err)
		}
		args = f.fs.Args()
		if len(args) == 0 {
			break
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	return pos, nil
}

func (f *flags) set(name string) bool {
	found := false
	f.fs.Visit(func(fl *flag.Flag) {
		if fl.Name == name {
			found = true
		}
	})
	return found
}

// request is a request ready to send.
type request struct {
	method      string
	path        string
	query       url.Values
	body        []byte
	contentType string
}

// buildPath fills the placeholders of a path from the positional arguments.
func buildPath(cmd command, pos []string) (string, error) {
	path := cmd.Path
	for i, name := range cmd.Args {
		if i >= len(pos) {
			break
		}
		val := pos[i]
		if name != "kind" && name != "name" {
			if n, err := strconv.ParseInt(val, 10, 64); err != nil || n < 1 {
				return "", usagef("the %s must be a positive number, not %q", name, val)
			}
		} else if name == "name" {
			val = strings.Trim(val, "/")
		}
		loc := placeholder.FindStringIndex(path)
		if loc == nil {
			return "", usagef("internal: too many arguments for %s", cmd.Path)
		}
		path = path[:loc[0]] + val + path[loc[1]:]
	}
	return path, nil
}

// readValue reads a flag value that may be @file.
func readValue(env Env, v string) (string, error) {
	if !strings.HasPrefix(v, "@") {
		return v, nil
	}
	name := v[1:]
	var b []byte
	var err error
	if name == "-" {
		b, err = io.ReadAll(env.Stdin)
	} else {
		b, err = os.ReadFile(name)
	}
	if err != nil {
		return "", usagef("cannot read %s: %v", name, err)
	}
	return string(b), nil
}

// parseLoRA reads `name[:model[:clip]]` into the object the API takes.
func parseLoRA(s string) (map[string]any, error) {
	parts := strings.Split(s, ":")
	out := map[string]any{"name": parts[0], "strength_model": 1.0, "strength_clip": 1.0}
	for i, key := range []string{"strength_model", "strength_clip"} {
		if len(parts) > i+1 && parts[i+1] != "" {
			n, err := strconv.ParseFloat(parts[i+1], 64)
			if err != nil {
				return nil, usagef("a LoRA is name[:model strength[:clip strength]], not %q", s)
			}
			out[key] = n
		}
	}
	if parts[0] == "" || len(parts) > 3 {
		return nil, usagef("a LoRA is name[:model strength[:clip strength]], not %q", s)
	}
	return out, nil
}

// parseModel reads `id[/version][:model[:clip]]` for a Civitai import.
func parseModel(s string) (map[string]any, error) {
	parts := strings.Split(s, ":")
	idText, versionText, hasVersion := strings.Cut(parts[0], "/")
	id, err := strconv.Atoi(idText)
	if err != nil || id < 1 || len(parts) > 3 {
		return nil, usagef("a model is id[/version][:model strength[:clip strength]], not %q", s)
	}
	out := map[string]any{"model_id": id}
	if hasVersion {
		version, err := strconv.Atoi(versionText)
		if err != nil || version < 1 {
			return nil, usagef("a model is id[/version][:model strength[:clip strength]], not %q", s)
		}
		out["version_id"] = version
	}
	for i, key := range []string{"model_strength", "model_clip"} {
		if len(parts) > i+1 && parts[i+1] != "" {
			n, err := strconv.ParseFloat(parts[i+1], 64)
			if err != nil {
				return nil, usagef("a model is id[/version][:model strength[:clip strength]], not %q", s)
			}
			out[key] = n
		}
	}
	return out, nil
}

// bodyOf collects the fields given as flags.
func (f *flags) bodyOf(cmd command, env Env) (map[string]any, error) {
	body := map[string]any{}
	for _, fl := range cmd.Body {
		if !f.set(fl.Flag) {
			continue
		}
		switch fl.Kind {
		case kString:
			body[fl.Key] = *f.strs[fl.Flag]
		case kText:
			v, err := readValue(env, *f.strs[fl.Flag])
			if err != nil {
				return nil, err
			}
			body[fl.Key] = v
		case kJSON:
			v, err := readValue(env, *f.strs[fl.Flag])
			if err != nil {
				return nil, err
			}
			if !json.Valid([]byte(v)) {
				return nil, usagef("--%s is not valid JSON", fl.Flag)
			}
			body[fl.Key] = json.RawMessage(v)
		case kInt:
			body[fl.Key] = *f.ints[fl.Flag]
		case kFloat:
			body[fl.Key] = *f.floats[fl.Flag]
		case kBool:
			body[fl.Key] = *f.bools[fl.Flag]
		case kList:
			var words []string
			for _, v := range f.lists[fl.Flag].values {
				words = append(words, strings.Split(v, ",")...)
			}
			switch fl.Key {
			case "loras":
				list := []map[string]any{}
				for _, w := range words {
					l, err := parseLoRA(strings.TrimSpace(w))
					if err != nil {
						return nil, err
					}
					list = append(list, l)
				}
				body[fl.Key] = list
			case "models":
				list := []map[string]any{}
				for _, w := range words {
					m, err := parseModel(strings.TrimSpace(w))
					if err != nil {
						return nil, err
					}
					list = append(list, m)
				}
				body[fl.Key] = list
			default:
				clean := []string{}
				for _, w := range words {
					if w = strings.TrimSpace(w); w != "" {
						clean = append(clean, w)
					}
				}
				body[fl.Key] = clean
			}
		}
	}
	return body, nil
}

// mergeFrom names the field of the answer that an editable field is read from,
// where the two differ.
var mergeFrom = map[string]string{"code": "command_code"}

// buildRequest turns the flags and arguments into a request.
func buildRequest(ctx context.Context, env Env, g globals, cmd command, f *flags, pos []string) (*request, error) {
	if cmd.Special == "code" && len(pos) == len(cmd.Args)+1 && !f.set("code") {
		// The command text may be the last argument.
		if err := f.fs.Set("code", pos[len(pos)-1]); err != nil { // also marks the flag as given
			return nil, usagef("%v", err)
		}
		pos = pos[:len(pos)-1]
	}
	if len(pos) != len(cmd.Args) {
		return nil, usagef("%s %s takes %s", cmd.Resource, cmd.Verb, argsText(cmd))
	}
	path, err := buildPath(cmd, pos)
	if err != nil {
		return nil, err
	}
	req := &request{method: cmd.Method, path: path, query: url.Values{}}
	for _, fl := range cmd.Query {
		if !f.set(fl.Flag) {
			continue
		}
		switch fl.Kind {
		case kInt:
			req.query.Set(fl.Key, strconv.FormatInt(*f.ints[fl.Flag], 10))
		default:
			req.query.Set(fl.Key, *f.strs[fl.Flag])
		}
	}
	if cmd.Paged {
		if *f.page > 0 {
			req.query.Set("page", strconv.Itoa(*f.page))
		}
		if *f.pageSize > 0 {
			req.query.Set("page_size", strconv.Itoa(*f.pageSize))
		}
	}
	if !takesBody(cmd) {
		return req, nil
	}

	body := map[string]any{}
	switch {
	case f.dataFile != nil && *f.dataFile != "":
		text, err := readFileOrStdin(env, *f.dataFile)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(text), &body); err != nil {
			return nil, usagef("--file is not a JSON object: %v", err)
		}
	case cmd.Merge:
		current, err := fetchCurrent(ctx, env, g, req.path)
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(cmd.Body)+1)
		for _, fl := range cmd.Body {
			keys = append(keys, fl.Key)
		}
		// A file that was uploaded earlier stays, unless a new one is sent.
		for _, fp := range cmd.Files {
			if fp.Part == "thumbnail" {
				keys = append(keys, "thumbnail")
			}
		}
		for _, key := range keys {
			if from, ok := mergeFrom[key]; ok {
				if v, ok := current[from]; ok {
					body[key] = v
				}
				continue
			}
			if v, ok := current[key]; ok {
				body[key] = v
			}
		}
	}
	given, err := f.bodyOf(cmd, env)
	if err != nil {
		return nil, err
	}
	for k, v := range given {
		body[k] = v
	}
	if !cmd.Merge && (f.dataFile == nil || *f.dataFile == "") {
		var missing []string
		for _, key := range cmd.Required {
			if _, ok := body[key]; !ok {
				missing = append(missing, "--"+flagOf(cmd, key))
			}
		}
		if len(missing) > 0 {
			return nil, usagef("%s %s needs %s", cmd.Resource, cmd.Verb, strings.Join(missing, ", "))
		}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, usagef("the request cannot be written: %v", err)
	}
	files := map[string][]string{}
	for _, fp := range cmd.Files {
		if vals := f.fileFlag[fp.Flag].values; len(vals) > 0 {
			files[fp.Flag] = vals
		}
	}
	if len(files) == 0 {
		req.body, req.contentType = data, "application/json"
		return req, nil
	}
	return multipartRequest(req, cmd, data, files)
}

func flagOf(cmd command, key string) string {
	for _, fl := range cmd.Body {
		if fl.Key == key {
			return fl.Flag
		}
	}
	return key
}

func argsText(cmd command) string {
	if len(cmd.Args) == 0 {
		return "no arguments"
	}
	words := make([]string, len(cmd.Args))
	for i, a := range cmd.Args {
		words[i] = strings.ToUpper(a)
	}
	return "the arguments " + strings.Join(words, " ")
}

func readFileOrStdin(env Env, path string) (string, error) {
	if path == "-" {
		b, err := io.ReadAll(env.Stdin)
		if err != nil {
			return "", usagef("cannot read standard input: %v", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", usagef("cannot read %s: %v", path, err)
	}
	return string(b), nil
}

// multipartRequest packs the JSON and the files into one multipart body.
func multipartRequest(req *request, cmd command, data []byte, files map[string][]string) (*request, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("data", string(data)); err != nil {
		return nil, usagef("%v", err)
	}
	flagsInOrder := make([]string, 0, len(files))
	for k := range files {
		flagsInOrder = append(flagsInOrder, k)
	}
	sort.Strings(flagsInOrder)
	for _, flagName := range flagsInOrder {
		var fp filePart
		for _, p := range cmd.Files {
			if p.Flag == flagName {
				fp = p
			}
		}
		for _, v := range files[flagName] {
			part, path := fp.Part, v
			if fp.Indexed {
				n, p, ok := strings.Cut(v, "=")
				if _, err := strconv.Atoi(n); !ok || err != nil {
					return nil, usagef("--%s is N=path, not %q", fp.Flag, v)
				}
				part, path = fp.Part+"_"+n, p
			}
			if err := addFile(mw, part, path); err != nil {
				return nil, err
			}
		}
	}
	if err := mw.Close(); err != nil {
		return nil, usagef("%v", err)
	}
	req.body, req.contentType = buf.Bytes(), mw.FormDataContentType()
	return req, nil
}

func addFile(mw *multipart.Writer, part, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return usagef("cannot read %s: %v", path, err)
	}
	defer func() { _ = f.Close() }() // read only
	w, err := mw.CreateFormFile(part, pathBase(path))
	if err != nil {
		return usagef("%v", err)
	}
	if _, err := io.Copy(w, f); err != nil {
		return usagef("cannot read %s: %v", path, err)
	}
	return nil
}

func pathBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// reply is an answer read to the end.
type reply struct {
	status int
	header http.Header
	body   []byte
}

// send sends a request. An error is an unreachable server.
func send(ctx context.Context, env Env, g globals, r *request) (*reply, error) {
	u := strings.TrimRight(g.server, "/") + "/api/v1" + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	hr, err := http.NewRequestWithContext(ctx, r.method, u, bytes.NewReader(r.body))
	if err != nil {
		return nil, usagef("the server address %q is not valid: %v", g.server, err)
	}
	if r.contentType != "" {
		hr.Header.Set("Content-Type", r.contentType)
	}
	if g.token != "" {
		hr.Header.Set("Authorization", "Bearer "+g.token)
	}
	res, err := env.Client.Do(hr)
	if err != nil {
		return nil, unreachable{wrapf(urlError(err), "cannot reach %s", g.server)}
	}
	defer func() { _ = res.Body.Close() }() // the body was read
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, unreachable{wrapf(err, "the connection to %s broke", g.server)}
	}
	return &reply{status: res.StatusCode, header: res.Header, body: b}, nil
}

// unreachable is a server that could not be reached.
type unreachable struct{ error }

// urlError drops the URL from a transport error; the address is in the message.
func urlError(err error) error {
	if ue, ok := err.(*url.Error); ok { //nolint:errorlint // only the outer error of the client is wanted
		return ue.Err
	}
	return err
}

// fetchCurrent reads a resource, for an update that changes only some fields.
func fetchCurrent(ctx context.Context, env Env, g globals, path string) (map[string]json.RawMessage, error) {
	rep, err := send(ctx, env, g, &request{method: "GET", path: path})
	if err != nil {
		return nil, err
	}
	if rep.status != http.StatusOK {
		return nil, apiError{rep}
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(rep.body, &out); err != nil {
		return nil, wrapf(err, "the server's answer is not JSON")
	}
	return out, nil
}

// apiError is an error answer.
type apiError struct{ rep *reply }

func (e apiError) Error() string { return fmt.Sprintf("the server answered %d", e.rep.status) }

// takesBody reports whether a command sends a JSON body.
func takesBody(cmd command) bool { return len(cmd.Body) > 0 || cmd.DataFile }
