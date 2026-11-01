package render

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nikolalohinski/gonja/v2"
	"github.com/nikolalohinski/gonja/v2/exec"

	"github.com/danilrwx/vpnctl-go/internal/state"
)

//go:embed templates/*.j2
var builtinTemplates embed.FS

// templateEnv holds templates: from the state's templates/ when present, built in otherwise.
type templateEnv struct{ m map[string]string }

func newTemplateEnv(st *state.State) (*templateEnv, error) {
	e := &templateEnv{m: map[string]string{}}

	paths, err := fs.Glob(builtinTemplates, "templates/*.j2")
	if err != nil {
		return nil, err
	}

	for _, p := range paths {
		name := filepath.Base(p)

		src, err := builtinTemplates.ReadFile(p)
		if err != nil {
			return nil, err
		}

		if over := filepath.Join(st.Dir, "templates", name); fileExists(over) {
			if src, err = os.ReadFile(over); err != nil {
				return nil, err
			}
		}

		e.m[name] = string(src)
	}

	return e, nil
}

// render executes a template; the result is normalized to end with a newline.
func (e *templateEnv) render(name string, ctx map[string]any) (string, error) {
	src, ok := e.m[name]
	if !ok {
		return "", fmt.Errorf("template %s is not built in", name)
	}

	tpl, err := gonja.FromString(src)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}

	s, err := tpl.ExecuteToString(exec.NewContext(ctx))
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}

	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}

	return s, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Template renders a built-in template (or its override in the state) with the given context.
func Template(st *state.State, name string, ctx map[string]any) (string, error) {
	e, err := newTemplateEnv(st)
	if err != nil {
		return "", err
	}

	return e.render(name, ctx)
}
