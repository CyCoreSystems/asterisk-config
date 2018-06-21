package template

import (
	"io"
	"io/ioutil"
	"text/template"
)

// Render processes the given input as a template, writing it to the provided output
func Render(e *Engine, in io.Reader, out io.Writer) error {

	buf, err := ioutil.ReadAll(in)
	if err != nil {
		return err
	}

	t, err := template.New("t").Parse(string(buf))
	if err != nil {
		return err
	}

	return t.Execute(out, e)
}
