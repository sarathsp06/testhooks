// Package cli is the testhooks command line for everything the UI and the MCP
// server can do: endpoints, requests, waiting, sending, previewing scripts.
// It is built on apiclient, so it offers the same operations with the same
// words. Output is JSON when stdout is not a terminal (a script or an agent
// is reading) and short text when it is; --json and --plain force either.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sarathsp06/testhooks/internal/apiclient"
	"github.com/sarathsp06/testhooks/skills"
)

// Exit codes. Scripts can branch on them.
const (
	ExitOK       = 0
	ExitError    = 1
	ExitUsage    = 2
	ExitTimedOut = 3 // requests wait: nothing arrived in time
)

const usage = `testhooks: webhook URLs that record every request sent to them.

Usage:
  testhooks                         run the server (see the docs for its environment)
  testhooks endpoints list|create|get|update|delete
  testhooks requests list|get|wait|send|replay|preview|delete
  testhooks mcp                     MCP server for AI agents, on stdin/stdout
  testhooks skill                   print the agent skill file (SKILL.md)
  testhooks version

The commands talk to a running Testhooks:
  --url URL        its address (default $TESTHOOKS_URL or http://localhost:8080)
  --token TOKEN    its AUTH_TOKEN or READ_TOKEN (default $TESTHOOKS_AUTH_TOKEN)
  --json           print JSON (the default when stdout is not a terminal)
  --plain          print short text (the default when it is)

Run "testhooks <command> -h" for its flags. Exit codes: 0 ok, 1 error, 2 usage, 3 timed out.
`

// Env is what Run needs from the process.
type Env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Version        string
	// IsTerminal says whether Stdout is a terminal; nil means "no".
	IsTerminal func() bool
	// Client overrides the one built from --url/--token, for tests.
	Client *apiclient.Client
}

// Run executes args (without the program name) and returns the exit code.
func Run(args []string, env Env) int {
	if env.Stdout == nil {
		env.Stdout = os.Stdout
	}
	if env.Stderr == nil {
		env.Stderr = os.Stderr
	}
	if env.Stdin == nil {
		env.Stdin = os.Stdin
	}
	a := &app{env: env}
	err := a.run(args)
	var ue usageError
	switch {
	case err == nil:
		return ExitOK
	case errors.Is(err, errTimedOut):
		return ExitTimedOut
	case errors.As(err, &ue):
		fmt.Fprintln(env.Stderr, "testhooks:", err)
		fmt.Fprintln(env.Stderr, "Run \"testhooks -h\" for usage.")
		return ExitUsage
	case errors.Is(err, flag.ErrHelp):
		return ExitOK
	default:
		fmt.Fprintln(env.Stderr, "testhooks:", err)
		return ExitError
	}
}

type usageError string

func (e usageError) Error() string { return string(e) }

var errTimedOut = errors.New("nothing arrived in time")

type app struct {
	env   Env
	url   string
	token string
	json  bool
	plain bool
	api   *apiclient.Client
}

// parse parses flags wherever they appear, before or after positional
// arguments, and returns the positionals: "update ep1 --mode browser" and
// "update --mode browser ep1" both work.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// flags returns a flag set for a command with the global flags on it, so
// they work before or after the command.
func (a *app) flags(name, help string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.env.Stderr)
	fs.Usage = func() {
		fmt.Fprint(a.env.Stderr, help)
		fs.PrintDefaults()
	}
	fs.StringVar(&a.url, "url", a.url, "address of the Testhooks to use")
	fs.StringVar(&a.token, "token", a.token, "its AUTH_TOKEN or READ_TOKEN")
	fs.BoolVar(&a.json, "json", a.json, "print JSON")
	fs.BoolVar(&a.plain, "plain", a.plain, "print short text")
	return fs
}

func (a *app) run(args []string) error {
	a.url = os.Getenv("TESTHOOKS_URL")
	a.token = os.Getenv("TESTHOOKS_AUTH_TOKEN")
	top := a.flags("testhooks", usage)
	if err := top.Parse(args); err != nil {
		return err
	}
	rest := top.Args()
	if len(rest) == 0 {
		return usageError("no command given")
	}
	switch rest[0] {
	case "version":
		fmt.Fprintln(a.env.Stdout, "testhooks", a.env.Version)
		return nil
	case "skill":
		_, err := io.WriteString(a.env.Stdout, skills.Testhooks)
		return err
	case "endpoints", "endpoint", "ep":
		return a.endpoints(rest[1:])
	case "requests", "request", "req":
		return a.requests(rest[1:])
	case "help", "-h", "--help":
		fmt.Fprint(a.env.Stdout, usage)
		return nil
	}
	return usageError(fmt.Sprintf("unknown command %q", rest[0]))
}

// client builds the API client once the flags are parsed.
func (a *app) client() *apiclient.Client {
	if a.api != nil {
		return a.api
	}
	if a.env.Client != nil {
		a.api = a.env.Client
		return a.api
	}
	base := a.url
	if base == "" {
		base = apiclient.DefaultURL
	}
	a.api = apiclient.New(base, a.token)
	return a.api
}

func (a *app) ctx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// wantJSON decides the output format.
func (a *app) wantJSON() bool {
	if a.json {
		return true
	}
	if a.plain {
		return false
	}
	return a.env.IsTerminal == nil || !a.env.IsTerminal()
}

// print writes v as JSON, or as text produced by text when that is wanted.
func (a *app) print(v any, text func(w io.Writer)) error {
	if a.wantJSON() {
		enc := json.NewEncoder(a.env.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	text(a.env.Stdout)
	return nil
}

// --- endpoints

func (a *app) endpoints(args []string) error {
	if len(args) == 0 {
		return usageError("endpoints: give list, create, get, update or delete")
	}
	switch args[0] {
	case "list", "ls":
		fs := a.flags("endpoints list", "Usage: testhooks endpoints list [--limit N]\n\n")
		limit := fs.Int("limit", 50, "how many to list, up to 200")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) > 0 {
			return usageError(fmt.Sprintf("unexpected argument %q", pos[0]))
		}
		ctx, stop := a.ctx()
		defer stop()
		out, err := a.client().ListEndpoints(ctx, apiclient.ListEndpointsInput{Limit: *limit})
		if err != nil {
			return err
		}
		return a.print(out, func(w io.Writer) {
			if len(out.Endpoints) == 0 {
				fmt.Fprintln(w, "No endpoints. Create one: testhooks endpoints create --slug stripe")
			}
			for _, ep := range out.Endpoints {
				printEndpoint(w, ep)
			}
		})
	case "create", "new":
		fs := a.flags("endpoints create", "Usage: testhooks endpoints create [--name S] [--slug S] [--mode server|browser] [--config JSON]\n\nA new endpoint only captures: forwarding, scripts and signature checks are off until set.\n\n")
		name := fs.String("name", "", "a label; doesn't change the URL")
		slug := fs.String("slug", "", "start of the URL, e.g. stripe gives /h/stripe-word-word-word")
		mode := fs.String("mode", "", "server (default) or browser")
		cfg := fs.String("config", "", "endpoint settings as JSON, e.g. '{\"forward_url\":\"https://example.com/hook\"}'")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) > 0 {
			return usageError(fmt.Sprintf("unexpected argument %q", pos[0]))
		}
		in := apiclient.CreateEndpointInput{Name: *name, SlugName: *slug, Mode: *mode}
		if *cfg != "" {
			if err := json.Unmarshal([]byte(*cfg), &in.Config); err != nil {
				return usageError("--config is not a JSON object: " + err.Error())
			}
		}
		ctx, stop := a.ctx()
		defer stop()
		ep, err := a.client().CreateEndpoint(ctx, in)
		if err != nil {
			return err
		}
		return a.print(ep, func(w io.Writer) {
			fmt.Fprintf(w, "Created endpoint %s (%s mode). Send webhooks to:\n  %s\n\nWait for the first one with:\n  testhooks requests wait %s\n", ep.ID, ep.Mode, ep.CaptureURL, ep.ID)
		})
	case "get", "show":
		fs := a.flags("endpoints get", "Usage: testhooks endpoints get <id|slug>\n\n")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("endpoints get: give an endpoint id or slug")
		}
		ctx, stop := a.ctx()
		defer stop()
		ep, err := a.resolve(ctx, pos[0])
		if err != nil {
			return err
		}
		return a.print(ep, func(w io.Writer) {
			printEndpoint(w, ep)
			cfg, _ := json.MarshalIndent(ep.Config, "  ", "  ")
			fmt.Fprintf(w, "  config: %s\n", cfg)
		})
	case "update", "set":
		fs := a.flags("endpoints update", "Usage: testhooks endpoints update <id> [--name S] [--slug S] [--mode M] [--config JSON]\n\n--config changes only the settings given and keeps the rest; a null removes one.\nA new --slug gives the endpoint a new URL and the old one stops working.\n\n")
		name := fs.String("name", "", "new label")
		slug := fs.String("slug", "", "new start of the URL (three new random words follow)")
		mode := fs.String("mode", "", "server or browser")
		cfg := fs.String("config", "", "settings to change, as JSON")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("endpoints update: give the endpoint id")
		}
		ctx, stop := a.ctx()
		defer stop()
		ep, err := a.resolve(ctx, pos[0])
		if err != nil {
			return err
		}
		if *cfg != "" {
			var patch map[string]any
			if err := json.Unmarshal([]byte(*cfg), &patch); err != nil {
				return usageError("--config is not a JSON object: " + err.Error())
			}
			if ep, err = a.client().UpdateConfig(ctx, apiclient.UpdateConfigInput{ID: ep.ID, Config: patch}); err != nil {
				return err
			}
		}
		fields := map[string]any{}
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "name":
				fields["name"] = *name
			case "slug":
				fields["slug_name"] = *slug
			case "mode":
				fields["mode"] = *mode
			}
		})
		if len(fields) > 0 {
			if err := a.client().Do(ctx, "PATCH", "/api/endpoints/"+ep.ID, fields, &ep); err != nil {
				return err
			}
			a.client().WithURL(&ep)
		}
		return a.print(ep, func(w io.Writer) { printEndpoint(w, ep) })
	case "delete", "rm":
		fs := a.flags("endpoints delete", "Usage: testhooks endpoints delete <id> [--yes]\n\nDeletes the endpoint and every request it received. Cannot be undone.\n\n")
		yes := fs.Bool("yes", false, "don't ask")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("endpoints delete: give the endpoint id")
		}
		ctx, stop := a.ctx()
		defer stop()
		ep, err := a.resolve(ctx, pos[0])
		if err != nil {
			return err
		}
		if !*yes {
			if a.env.IsTerminal == nil || !a.env.IsTerminal() {
				return usageError("endpoints delete: pass --yes to delete without asking")
			}
			fmt.Fprintf(a.env.Stderr, "Delete endpoint %s (%s) and all its requests? This cannot be undone. [y/N] ", ep.ID, ep.CaptureURL)
			line, _ := bufio.NewReader(a.env.Stdin).ReadString('\n')
			if l := strings.ToLower(strings.TrimSpace(line)); l != "y" && l != "yes" {
				fmt.Fprintln(a.env.Stderr, "Kept.")
				return nil
			}
		}
		out, err := a.client().DeleteEndpoint(ctx, apiclient.DeleteEndpointInput{ID: ep.ID})
		if err != nil {
			return err
		}
		return a.print(out, func(w io.Writer) { fmt.Fprintf(w, "Deleted endpoint %s.\n", out.Deleted) })
	}
	return usageError(fmt.Sprintf("endpoints: unknown command %q", args[0]))
}

// resolve finds an endpoint by id, or by the slug in its capture URL.
func (a *app) resolve(ctx context.Context, ref string) (apiclient.Endpoint, error) {
	ep, err := a.client().GetEndpoint(ctx, apiclient.GetEndpointInput{ID: ref})
	if err == nil || !strings.Contains(err.Error(), " 404 ") {
		return ep, err
	}
	ep, err2 := a.client().GetEndpoint(ctx, apiclient.GetEndpointInput{Slug: ref})
	if err2 != nil {
		return ep, fmt.Errorf("no endpoint with id or slug %q", ref)
	}
	return ep, nil
}

func printEndpoint(w io.Writer, ep apiclient.Endpoint) {
	name := ep.Name
	if name == "" {
		name = "(no name)"
	}
	fmt.Fprintf(w, "%s  %s  %s  %s\n", ep.CaptureURL, ep.ID, ep.Mode, name)
}

// --- requests

func (a *app) requests(args []string) error {
	if len(args) == 0 {
		return usageError("requests: give list, get, wait, send, replay, preview or delete")
	}
	switch args[0] {
	case "list", "ls":
		fs := a.flags("requests list", "Usage: testhooks requests list <endpoint> [--limit N] [--after ID]\n\n")
		limit := fs.Int("limit", 50, "how many, newest first, up to 200")
		after := fs.String("after", "", "only requests newer than this request id")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("requests list: give the endpoint id or slug")
		}
		ctx, stop := a.ctx()
		defer stop()
		ep, err := a.resolve(ctx, pos[0])
		if err != nil {
			return err
		}
		out, err := a.client().ListRequests(ctx, apiclient.ListRequestsInput{EndpointID: ep.ID, Limit: *limit, After: *after})
		if err != nil {
			return err
		}
		return a.print(out, func(w io.Writer) {
			if len(out.Requests) == 0 {
				fmt.Fprintf(w, "No requests yet. Send one to %s, or wait: testhooks requests wait %s\n", ep.CaptureURL, ep.ID)
			}
			for _, r := range out.Requests {
				printRequestLine(w, r)
			}
		})
	case "get", "show":
		fs := a.flags("requests get", "Usage: testhooks requests get <request id>\n\n")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("requests get: give the request id")
		}
		ctx, stop := a.ctx()
		defer stop()
		out, err := a.client().GetRequest(ctx, apiclient.GetRequestInput{ID: pos[0]})
		if err != nil {
			return err
		}
		return a.print(out, func(w io.Writer) { printRequest(w, out) })
	case "wait":
		fs := a.flags("requests wait", "Usage: testhooks requests wait <endpoint> [--after ID] [--timeout SECONDS]\n\nWaits until the endpoint receives a request and prints it. Exits 3 when nothing arrived in time.\n\n")
		after := fs.String("after", "", "the last request id you saw: wait only for newer ones")
		timeout := fs.Int("timeout", 60, "seconds to wait, up to 300")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("requests wait: give the endpoint id or slug")
		}
		ctx, stop := a.ctx()
		defer stop()
		ep, err := a.resolve(ctx, pos[0])
		if err != nil {
			return err
		}
		if !a.wantJSON() {
			fmt.Fprintf(a.env.Stderr, "Waiting up to %ds for a request to %s ...\n", *timeout, ep.CaptureURL)
		}
		out, err := a.client().WaitForRequest(ctx, apiclient.WaitForRequestInput{EndpointID: ep.ID, After: *after, TimeoutSeconds: *timeout})
		if err != nil {
			return err
		}
		if err := a.print(out, func(w io.Writer) {
			for _, r := range out.Requests {
				printRequest(w, r)
			}
		}); err != nil {
			return err
		}
		if out.TimedOut {
			return errTimedOut
		}
		return nil
	case "send":
		fs := a.flags("requests send", "Usage: testhooks requests send <endpoint> [-X METHOD] [-p PATH] [-H 'Name: value']... [-d BODY|@FILE|-] [--content-type TYPE]\n\nSends a test request to the endpoint's capture URL, as a provider would, and prints what the sender got back and what was captured.\n\n")
		method := fs.String("X", "POST", "HTTP method")
		path := fs.String("p", "", "path after the capture URL, e.g. /stripe/events")
		var headers headerFlags
		fs.Var(&headers, "H", "a header, 'Name: value'; repeatable")
		data := fs.String("d", "", "the body; @FILE reads a file, - reads stdin")
		ct := fs.String("content-type", "", "Content-Type (JSON bodies get application/json by default)")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("requests send: give the endpoint id or slug")
		}
		body := *data
		switch {
		case body == "-":
			b, err := io.ReadAll(a.env.Stdin)
			if err != nil {
				return err
			}
			body = string(b)
		case strings.HasPrefix(body, "@"):
			b, err := os.ReadFile(body[1:])
			if err != nil {
				return err
			}
			body = string(b)
		}
		ctx, stop := a.ctx()
		defer stop()
		ep, err := a.resolve(ctx, pos[0])
		if err != nil {
			return err
		}
		out, err := a.client().SendRequest(ctx, apiclient.SendRequestInput{EndpointID: ep.ID, Method: *method, Path: *path, Headers: headers, Body: body, ContentType: *ct})
		if err != nil {
			return err
		}
		return a.print(out, func(w io.Writer) {
			fmt.Fprintf(w, "%s %s -> %d\n%s\n", strings.ToUpper(*method), out.URL, out.Status, strings.TrimSpace(out.Reply))
			if id, _ := out.Request["id"].(string); id != "" {
				fmt.Fprintf(w, "Captured as request %s\n", id)
			}
		})
	case "replay":
		fs := a.flags("requests replay", "Usage: testhooks requests replay <request id>\n\nSends a captured request to its endpoint again, as if the provider sent it twice.\n\n")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("requests replay: give the request id")
		}
		ctx, stop := a.ctx()
		defer stop()
		out, err := a.client().ReplayRequest(ctx, apiclient.ReplayRequestInput{ID: pos[0]})
		if err != nil {
			return err
		}
		return a.print(out, func(w io.Writer) { fmt.Fprintf(w, "%s -> %d\n%s\n", out.URL, out.Status, strings.TrimSpace(out.Body)) })
	case "preview", "try":
		fs := a.flags("requests preview", "Usage: testhooks requests preview <endpoint> [--request ID] [--transform FILE] [--reply FILE]\n\nRuns the scripts on a captured request (the newest, or --request) without saving or sending anything, and prints what would be forwarded and what the sender would get. FILE may be - for stdin; an empty value runs no such script.\n\n")
		reqID := fs.String("request", "", "the captured request to try the scripts on (default: the newest)")
		tf := fs.String("transform", "", "file with a transform(req) script; left out, the saved one runs")
		rf := fs.String("reply", "", "file with a handler(req) script; left out, the saved one runs")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("requests preview: give the endpoint id or slug")
		}
		in := apiclient.PreviewScriptInput{RequestID: *reqID}
		var ferr error
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "transform":
				in.Transform, ferr = readScript(*tf, a.env.Stdin, ferr)
			case "reply":
				in.Reply, ferr = readScript(*rf, a.env.Stdin, ferr)
			}
		})
		if ferr != nil {
			return ferr
		}
		ctx, stop := a.ctx()
		defer stop()
		ep, err := a.resolve(ctx, pos[0])
		if err != nil {
			return err
		}
		in.EndpointID = ep.ID
		out, err := a.client().PreviewScript(ctx, in)
		if err != nil {
			return err
		}
		return a.print(out, func(w io.Writer) { printPreview(w, out) })
	case "delete", "rm":
		fs := a.flags("requests delete", "Usage: testhooks requests delete <request id>\n\n")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usageError("requests delete: give the request id")
		}
		ctx, stop := a.ctx()
		defer stop()
		if err := a.client().Do(ctx, "DELETE", "/api/requests/"+pos[0], nil, nil); err != nil {
			return err
		}
		return a.print(map[string]string{"deleted": pos[0]}, func(w io.Writer) { fmt.Fprintf(w, "Deleted request %s.\n", pos[0]) })
	}
	return usageError(fmt.Sprintf("requests: unknown command %q", args[0]))
}

// readScript reads a script flag's value: a file, - for stdin, or "" for none.
func readScript(v string, stdin io.Reader, prev error) (*string, error) {
	if prev != nil {
		return nil, prev
	}
	switch v {
	case "":
		return &v, nil
	case "-":
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, err
		}
		s := string(b)
		return &s, nil
	}
	b, err := os.ReadFile(v)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

// headerFlags collects repeated -H 'Name: value' flags.
type headerFlags map[string]string

func (h *headerFlags) String() string { return "" }

func (h *headerFlags) Set(v string) error {
	name, value, ok := strings.Cut(v, ":")
	if !ok || strings.TrimSpace(name) == "" {
		return fmt.Errorf("want 'Name: value', got %q", v)
	}
	if *h == nil {
		*h = map[string]string{}
	}
	(*h)[strings.TrimSpace(name)] = strings.TrimSpace(value)
	return nil
}

// --- text output

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func printRequestLine(w io.Writer, r map[string]any) {
	size, _ := r["size"].(float64)
	at := str(r, "created_at")
	if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
		at = t.Local().Format("15:04:05")
	}
	sig := ""
	if s, ok := r["signature"].(map[string]any); ok {
		sig = "  signature " + str(s, "status")
	}
	fmt.Fprintf(w, "%s  %-6s %-24s %6sB  %s%s\n", at, str(r, "method"), str(r, "path"), strconv.Itoa(int(size)), str(r, "id"), sig)
}

func printRequest(w io.Writer, r map[string]any) {
	printRequestLine(w, r)
	if h, ok := r["headers"].(map[string]any); ok {
		for k, vs := range h {
			if list, ok := vs.([]any); ok {
				for _, v := range list {
					fmt.Fprintf(w, "  %s: %v\n", k, v)
				}
			}
		}
	}
	if body := str(r, "body"); body != "" {
		if str(r, "body_encoding") == "base64" {
			fmt.Fprintf(w, "\n  (binary body, %d bytes base64; use --json for it)\n", len(body))
		} else {
			fmt.Fprintf(w, "\n%s\n", body)
		}
	}
	if fr, ok := r["forward_result"].(map[string]any); ok {
		if fr["in_progress"] == true {
			fmt.Fprintf(w, "\nForwarding to %s ...\n", str(fr, "url"))
		} else {
			code, _ := fr["status_code"].(float64)
			fmt.Fprintf(w, "\nForwarded to %s -> %d %s\n", str(fr, "url"), int(code), str(fr, "error"))
		}
	}
	if ri, ok := r["response_info"].(map[string]any); ok {
		code, _ := ri["status"].(float64)
		fmt.Fprintf(w, "Sender got %d (%s)\n", int(code), str(ri, "source"))
	}
	if c := str(r, "curl"); c != "" {
		fmt.Fprintf(w, "\nSend it again:\n%s\n", c)
	}
}

func printPreview(w io.Writer, out map[string]any) {
	if t, ok := out["transform"].(map[string]any); ok {
		switch {
		case str(t, "error") != "":
			fmt.Fprintf(w, "Transform threw: %s\n  The sender would get the default reply.\n", str(t, "error"))
		case t["dropped"] == true:
			fmt.Fprintln(w, "Transform skipped the request: nothing would be forwarded.")
		default:
			if f, ok := t["forwarded"].(map[string]any); ok {
				fmt.Fprintf(w, "Would forward %s %s\n%s\n", str(f, "method"), str(f, "path"), str(f, "body"))
			}
		}
	} else {
		fmt.Fprintln(w, "No transform: the request would be forwarded as it is.")
	}
	if r, ok := out["reply"].(map[string]any); ok {
		code, _ := r["status"].(float64)
		fmt.Fprintf(w, "\nThe sender would get %d (%s)\n%s\n", int(code), str(r, "source"), str(r, "body"))
		if e := str(r, "error"); e != "" {
			fmt.Fprintf(w, "Reply script threw: %s\n", e)
		}
	}
}
