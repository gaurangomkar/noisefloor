package report

import (
	"encoding/json"
	"io"

	"github.com/gaurangomkar/noisefloor/internal/model"
)

type jsonReporter struct{}

func (jsonReporter) Format() string { return "json" }

func (jsonReporter) Render(w io.Writer, rep model.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rep); err != nil {
		return err
	}
	return nil
}
