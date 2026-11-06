package zed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tailscale/hujson"

	"github.com/mcpfleet/mcpfleet/internal/adapters/jsonfile"
	"github.com/mcpfleet/mcpfleet/internal/schema"
)

// zedSettings mirrors Zed's ~/.config/zed/settings.json (partial).
// Zed stores MCP context servers under the "context_servers" key.
type zedSettings struct {
	ContextServers map[string]contextServer `json:"context_servers"`
}

// contextServer is one "context_servers" entry as List reads it. Zed's
// schema (ContextServerSettingsContent in crates/settings_content/src/project.rs)
// has a flat stdio entry: "command" is the executable, with "args" and "env"
// beside it. Older settings nest them as "command": {"path", "args", "env"}
// (Zed's m_2025_06_27 migration flattens those), so "command" is kept raw and
// decoded by its JSON type.
type contextServer struct {
	Command json.RawMessage   `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	URL     string            `json:"url,omitempty"`     // remote (http) servers only
	Headers map[string]string `json:"headers,omitempty"` // remote (http) servers only
}

// localServer is the flat stdio entry Apply writes.
type localServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// remoteServer is the entry Apply writes for a remote server: Zed's HTTP
// context server, told apart from a local one by its "url" alone, with its
// HTTP headers beside it (the Http variant of ContextServerSettings in
// crates/project/src/project_settings.rs, and docs/src/ai/mcp.md). Without an
// "Authorization" header Zed starts the MCP OAuth flow.
type remoteServer struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// contextCommand is the nested "command" object of the old entry format.
// List decodes both formats into it (see decodeCommand); Apply never writes it.
type contextCommand struct {
	Path string            `json:"path"`
	Args []string          `json:"args,omitempty"`
	Env  map[string]string `json:"env,omitempty"`
}

// Adapter implements adapters.Adapter for Zed editor.
type Adapter struct{}

func New() *Adapter { return &Adapter{} }

func (a *Adapter) Name() string { return "zed" }

func (a *Adapter) ConfigPath() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Zed", "settings.json")
	default:
		return filepath.Join(home, ".config", "zed", "settings.json")
	}
}

// path is ConfigPath for reading and writing: it refuses a relative path,
// which an empty APPDATA (Windows) or home directory gives, so the config is
// never resolved against the working directory.
func (a *Adapter) path() (string, error) {
	v := jsonfile.HomeVar(runtime.GOOS)
	if runtime.GOOS == "windows" {
		v = "APPDATA"
	}
	return jsonfile.RequireAbs(a.Name(), a.ConfigPath(), v)
}

// SupportsTransport reports whether the config file can hold a server with
// the given transport: stdio (also the empty transport) and http. Zed has no
// SSE transport for context servers.
func (a *Adapter) SupportsTransport(transport string) bool {
	switch transport {
	case "", schema.TransportStdio, schema.TransportHTTP:
		return true
	}
	return false
}

// Apply adds or replaces the given servers under "context_servers" in Zed's
// settings.json. The file is JSONC (comments, trailing commas), so it is
// patched in place on its HuJSON syntax tree: comments, formatting and every
// other setting survive. An unparsable file is an error and is not written.
// A local server is written in Zed's flat format ({"command", "args", "env"});
// a remote one as {"url"}.
func (a *Adapter) Apply(servers []schema.Server) error {
	given := len(servers)
	servers = a.writable(servers)
	if len(servers) == 0 && given > 0 {
		return nil // every server was skipped: leave the file alone
	}
	cfgPath, err := a.path()
	if err != nil {
		return err
	}
	prev, err := jsonfile.Read(cfgPath)
	if err != nil {
		return fmt.Errorf("zed: %w", err)
	}
	src := prev
	if len(bytes.TrimSpace(src)) == 0 {
		src = []byte("{}\n")
	}
	v, err := hujson.Parse(src)
	if err != nil {
		return fmt.Errorf("zed: parse %s: %w", cfgPath, err)
	}

	// Check the shape on a standardized copy before patching.
	std := v.Clone()
	std.Standardize()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(std.Pack(), &root); err != nil || root == nil {
		return fmt.Errorf("zed: parse %s: root is not a JSON object", cfgPath)
	}
	type patchOp struct {
		Op    string          `json:"op"`
		Path  string          `json:"path"`
		Value json.RawMessage `json:"value"`
	}
	var ops []patchOp
	var cs map[string]json.RawMessage
	existing, ok := root["context_servers"]
	csAdded := !ok || bytes.Equal(bytes.TrimSpace(existing), []byte("null"))
	if csAdded {
		ops = append(ops, patchOp{Op: "add", Path: "/context_servers", Value: json.RawMessage("{}")})
	} else if err := json.Unmarshal(existing, &cs); err != nil {
		return fmt.Errorf("zed: parse %s: \"context_servers\" is not a JSON object", cfgPath)
	}

	for _, s := range servers {
		var entry any = remoteServer{URL: s.URL, Headers: schema.Values(s.Headers)}
		if !s.IsRemote() {
			local := localServer{Command: s.Command, Args: s.Args}
			if len(s.Env) > 0 {
				local.Env = make(map[string]string, len(s.Env))
				for k, v := range s.Env {
					local.Env[k] = v.Value
				}
			}
			entry = local
		}
		enc, err := jsonfile.Marshal(entry, "", "")
		if err != nil {
			return fmt.Errorf("zed: encode server %q: %w", s.Name, err)
		}
		ops = append(ops, patchOp{Op: "add", Path: "/context_servers/" + pointerEscape(s.Name), Value: enc})
	}
	patch, err := jsonfile.Marshal(ops, "", "")
	if err != nil {
		return fmt.Errorf("zed: encode patch: %w", err)
	}
	if err := v.Patch(patch); err != nil {
		return fmt.Errorf("zed: patch %s: %w", cfgPath, err)
	}

	defaultIndent := ""
	if len(root) == 0 {
		// Nothing to take the indentation from: use Zed's two spaces.
		defaultIndent = "  "
	}
	if err := layout(&v, servers, cs, csAdded, defaultIndent); err != nil {
		return fmt.Errorf("zed: %w", err)
	}

	if err := jsonfile.WriteFile(cfgPath, v.Pack(), prev); err != nil {
		return fmt.Errorf("zed: %w", err)
	}
	return nil
}

// writable returns the servers Apply can write: the ones whose transport Zed
// supports, without remote servers that have no URL.
func (a *Adapter) writable(servers []schema.Server) []schema.Server {
	var out []schema.Server
	for _, s := range servers {
		if !a.SupportsTransport(s.Transport) || (s.IsRemote() && s.URL == "") {
			continue // never write an entry with an empty command
		}
		out = append(out, s)
	}
	return out
}

// layout indents what Apply added or replaced so it matches the rest of the
// file: new members start on their own line at their siblings' indentation
// and server values are pretty-printed. Comments and untouched members keep
// their original bytes. A file without line-based indentation (e.g. a
// one-line object) is left compact unless defaultIndent is set.
func layout(v *hujson.Value, servers []schema.Server, before map[string]json.RawMessage, csAdded bool, defaultIndent string) error {
	root, ok := v.Value.(*hujson.Object)
	if !ok {
		return nil
	}
	rootIndent, ok := memberIndent(root, func(name string) bool { return !(csAdded && name == "context_servers") })
	if !ok {
		if defaultIndent == "" {
			return nil
		}
		rootIndent = defaultIndent
	}
	if rootIndent == "" {
		return nil
	}
	unit := rootIndent

	var csMember *hujson.ObjectMember
	for i := range root.Members {
		if memberName(root.Members[i]) == "context_servers" {
			csMember = &root.Members[i]
		}
	}
	if csMember == nil {
		return nil
	}
	csObj, ok := csMember.Value.Value.(*hujson.Object)
	if !ok {
		return nil
	}
	if csAdded {
		csMember.Name.BeforeExtra = onNewLine(csMember.Name.BeforeExtra, rootIndent)
		csMember.Value.BeforeExtra = hujson.Extra(" ")
		csObj.AfterExtra = hujson.Extra("\n" + rootIndent)
		if !bytes.Contains(root.AfterExtra, []byte("\n")) {
			root.AfterExtra = append(append(hujson.Extra(nil), root.AfterExtra...), '\n')
		}
	}

	applied := make(map[string]bool, len(servers))
	for _, s := range servers {
		applied[s.Name] = true
	}
	csIndent, ok := memberIndent(csObj, func(name string) bool { return !applied[name] || before[name] != nil })
	if !ok {
		csIndent = rootIndent + unit
	}
	for i := range csObj.Members {
		m := &csObj.Members[i]
		name := memberName(*m)
		if !applied[name] {
			continue
		}
		if before[name] == nil {
			m.Name.BeforeExtra = onNewLine(m.Name.BeforeExtra, csIndent)
			m.Value.BeforeExtra = hujson.Extra(" ")
		}
		std := m.Value.Clone()
		std.Standardize()
		// json.Indent only re-indents; unlike MarshalIndent it does not
		// escape &, < and >.
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, std.Pack(), csIndent, unit); err != nil {
			return fmt.Errorf("format server %q: %w", name, err)
		}
		pv, err := hujson.Parse(pretty.Bytes())
		if err != nil {
			return fmt.Errorf("format server %q: %w", name, err)
		}
		m.Value.Value = pv.Value
	}
	return nil
}

// memberIndent returns the indentation of the first member accepted by keep
// that starts on its own line.
func memberIndent(obj *hujson.Object, keep func(name string) bool) (string, bool) {
	for _, m := range obj.Members {
		if !keep(memberName(m)) {
			continue
		}
		extra := m.Name.BeforeExtra
		i := bytes.LastIndexByte(extra, '\n')
		if i < 0 {
			continue
		}
		tail := extra[i+1:]
		n := 0
		for n < len(tail) && (tail[n] == ' ' || tail[n] == '\t') {
			n++
		}
		return string(tail[:n]), true
	}
	return "", false
}

// onNewLine keeps any comments in extra and makes it end with a line break
// followed by indent.
func onNewLine(extra hujson.Extra, indent string) hujson.Extra {
	// Copy: extra may share its backing array with neighbouring nodes.
	out := append(hujson.Extra(nil), bytes.TrimRight(extra, " \t")...)
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	return append(out, indent...)
}

func memberName(m hujson.ObjectMember) string {
	if lit, ok := m.Name.Value.(hujson.Literal); ok {
		return lit.String()
	}
	return ""
}

// pointerEscape escapes a name for use as a JSON Pointer token (RFC 6901).
func pointerEscape(s string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(s)
}

func (a *Adapter) List() ([]schema.Server, error) {
	p, err := a.path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("zed: read config: %w", err)
	}
	// Zed writes settings.json as JSONC (comments, trailing commas).
	std, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("zed: parse config: %w", err)
	}
	var cfg zedSettings
	if err := json.Unmarshal(std, &cfg); err != nil {
		return nil, fmt.Errorf("zed: parse config: %w", err)
	}
	var servers []schema.Server
	for name, cs := range cfg.ContextServers {
		cmd, err := decodeCommand(cs)
		if err != nil {
			return nil, fmt.Errorf("zed: parse config: server %q: %w", name, err)
		}
		env := make(map[string]schema.EnvVar, len(cmd.Env))
		for k, v := range cmd.Env {
			env[k] = schema.EnvVar{Value: v}
		}
		srv := schema.Server{
			Name:    name,
			Command: cmd.Path,
			Args:    cmd.Args,
			Env:     env,
		}
		if cs.URL != "" && cmd.Path == "" {
			srv.Transport = schema.TransportHTTP
			srv.URL = cs.URL
			srv.Headers = schema.Literals(cs.Headers)
		}
		servers = append(servers, srv)
	}
	return servers, nil
}

// decodeCommand reads the command of an entry in either format: a string
// "command" is the flat format (args and env sit beside it), an object is the
// old nested format. A missing or null "command" is a remote or extension
// entry and gives an empty command. Any other JSON type is an error.
func decodeCommand(cs contextServer) (contextCommand, error) {
	raw := bytes.TrimSpace(cs.Command)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return contextCommand{}, nil
	}
	switch raw[0] {
	case '"':
		var path string
		if err := json.Unmarshal(raw, &path); err != nil {
			return contextCommand{}, err
		}
		return contextCommand{Path: path, Args: cs.Args, Env: cs.Env}, nil
	case '{':
		var cmd contextCommand
		if err := json.Unmarshal(raw, &cmd); err != nil {
			return contextCommand{}, err
		}
		return cmd, nil
	}
	return contextCommand{}, errors.New(`"command" is neither a string nor an object`)
}
