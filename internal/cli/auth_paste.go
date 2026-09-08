package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/steipete/spogo/internal/app"
	"github.com/steipete/spogo/internal/output"
)

type pastedCookies struct {
	spdc  string
	spkey string
	spt   string
}

func (cmd *AuthPasteCmd) Run(ctx *app.Context) error {
	stdinIsTTY := isatty.IsTerminal(os.Stdin.Fd())
	if ctx.Settings.NoInput && stdinIsTTY {
		return errors.New("--no-input set; pipe cookie values via stdin")
	}
	values, err := readPastedCookies(os.Stdin, ctx.Output, stdinIsTTY && !ctx.Settings.NoInput)
	if err != nil {
		return err
	}
	if values.spdc == "" {
		return errors.New("sp_dc required")
	}
	cookiesList := buildPastedCookies(values, normalizeCookieDomain(cmd.Domain), normalizeCookiePath(cmd.Path))
	if values.spt == "" && warnsOnMissingDeviceCookie(ctx.Profile.Engine) {
		_, _ = fmt.Fprintln(ctx.Output.Err, "warning: missing sp_t; playback may fail (grab sp_t from DevTools)")
	}
	return saveCookies(ctx, cmd.CookiePath, cookiesList, nil)
}

func readPastedCookies(r io.Reader, out *output.Writer, interactive bool) (pastedCookies, error) {
	if interactive {
		return promptPastedCookies(out)
	}
	return parsePastedCookies(r)
}

func promptPastedCookies(out *output.Writer) (pastedCookies, error) {
	reader := bufio.NewReader(os.Stdin)
	spdc, err := readPromptCookieValue(reader, out, "sp_dc", true)
	if err != nil {
		return pastedCookies{}, err
	}
	spkey, err := readPromptCookieValue(reader, out, "sp_key", false)
	if err != nil {
		return pastedCookies{}, err
	}
	spt, err := readPromptCookieValue(reader, out, "sp_t", false)
	if err != nil {
		return pastedCookies{}, err
	}
	return pastedCookies{spdc: spdc, spkey: spkey, spt: spt}, nil
}

func parsePastedCookies(r io.Reader) (pastedCookies, error) {
	if r == nil {
		r = os.Stdin
	}
	scanner := bufio.NewScanner(r)
	values := pastedCookies{}
	for scanner.Scan() {
		line := scanner.Text()
		if value, ok := extractNamedCookieValue(line, "sp_dc"); ok {
			values.spdc = value
		}
		if value, ok := extractNamedCookieValue(line, "sp_key"); ok {
			values.spkey = value
		}
		if value, ok := extractNamedCookieValue(line, "sp_t"); ok {
			values.spt = value
		}
	}
	if err := scanner.Err(); err != nil {
		return pastedCookies{}, err
	}
	return values, nil
}

func readPromptCookieValue(reader *bufio.Reader, out *output.Writer, name string, required bool) (string, error) {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	if out != nil {
		_, _ = fmt.Fprintf(out.Err, "Paste %s value: ", name)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value := normalizePromptCookieValue(line, name)
	if value == "" && required {
		return "", fmt.Errorf("%s required", name)
	}
	return value, nil
}

func buildPastedCookies(values pastedCookies, domain, path string) []*http.Cookie {
	cookiesList := []*http.Cookie{newCookie("sp_dc", values.spdc, domain, path)}
	if values.spkey != "" {
		cookiesList = append(cookiesList, newCookie("sp_key", values.spkey, domain, path))
	}
	if values.spt != "" {
		cookiesList = append(cookiesList, newCookie("sp_t", values.spt, domain, path))
	}
	return cookiesList
}

func newCookie(name, value, domain, path string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Domain:   domain,
		Path:     path,
		Secure:   true,
		HttpOnly: true,
	}
}

func warnsOnMissingDeviceCookie(engine string) bool {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "", "connect", "auto":
		return true
	default:
		return false
	}
}

func normalizeCookieDomain(domain string) string {
	trimmed := strings.TrimSpace(domain)
	if trimmed == "" {
		trimmed = "spotify.com"
	}
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Hostname() != "" {
			trimmed = parsed.Hostname()
		}
	}
	if !strings.HasPrefix(trimmed, ".") {
		trimmed = "." + trimmed
	}
	return trimmed
}

func normalizeCookiePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "/"
	}
	return trimmed
}

func normalizePromptCookieValue(value, name string) string {
	if parsed, ok := extractNamedCookieValue(value, name); ok {
		return parsed
	}
	return trimCookieValue(value)
}

func extractNamedCookieValue(value, name string) (string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(value), "\"'")
	if trimmed == "" {
		return "", false
	}
	for _, part := range strings.Split(trimmed, ";") {
		part = strings.TrimSpace(part)
		key, val, found := strings.Cut(part, "=")
		if !found {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return trimCookieValue(val), true
		}
	}
	return "", false
}

func trimCookieValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\"'")
}
