package aigovernance

import (
	"fmt"
	"io"
	"strings"
)

type Logger struct {
	Out io.Writer
	Err io.Writer
}

func (l Logger) Info(format string, args ...any) {
	if l.Out == nil {
		return
	}
	fmt.Fprintf(l.Out, strings.TrimSuffix(format, "\n")+"\n", args...)
}

func (l Logger) Warning(format string, args ...any) {
	if l.Err == nil {
		return
	}
	fmt.Fprintf(l.Err, "::warning::"+strings.TrimSuffix(format, "\n")+"\n", args...)
}

func (l Logger) Error(format string, args ...any) {
	if l.Err == nil {
		return
	}
	fmt.Fprintf(l.Err, "::error::"+strings.TrimSuffix(format, "\n")+"\n", args...)
}

func (l Logger) LogTemplate(key string, config Config, values map[string]string) {
	if config.Logging != nil {
		l.Info(Render(config.Logging[key], values))
	}
}
