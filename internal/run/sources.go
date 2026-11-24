package run

import (
	"fmt"
	"net/http"

	"github.com/gaurangomkar/noisefloor/internal/config"
	"github.com/gaurangomkar/noisefloor/internal/source"
	"github.com/gaurangomkar/noisefloor/internal/source/alertmanager"
	"github.com/gaurangomkar/noisefloor/internal/source/jsonfile"
	"github.com/gaurangomkar/noisefloor/internal/source/pagerduty"
	"github.com/gaurangomkar/noisefloor/internal/source/sqlite"
)

// openSources constructs the sources enabled in cfg. A nil PagerDuty block
// stays disabled; the other blocks are omitted the same way.
func openSources(cfg config.Config) ([]source.Source, error) {
	var out []source.Source
	if am := cfg.Sources.Alertmanager; am != nil {
		out = append(out, alertmanager.Client{
			BaseURL:          am.URL,
			Username:         am.Username,
			Password:         am.Password,
			BearerToken:      am.BearerToken,
			IncludeSilenced:  am.IncludeSilenced,
			IncludeInhibited: am.IncludeInhibited,
			HTTP:             &http.Client{Timeout: am.Timeout},
		})
	}
	if cfg.Sources.SQLite != nil {
		out = append(out, sqlite.File{Path: cfg.Sources.SQLite.Path})
	}
	for _, f := range cfg.Sources.Files {
		out = append(out, jsonfile.Source{Path: f.Path, Label: f.Name})
	}
	if pd := cfg.Sources.PagerDuty; pd != nil {
		out = append(out, pagerduty.Client{
			Token:      pd.Token,
			BaseURL:    pd.BaseURL,
			ServiceIDs: pd.ServiceIDs,
			HTTP:       &http.Client{Timeout: pd.Timeout},
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no sources configured")
	}
	return out, nil
}
