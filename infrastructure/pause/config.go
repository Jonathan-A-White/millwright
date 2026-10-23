package pause

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Jonathan-A-White/millwright/application"
	"github.com/Jonathan-A-White/millwright/domain"
	"github.com/Jonathan-A-White/millwright/infrastructure/configfile"
)

// MaxBoxesKey is the config key a pause sets to 0.
const MaxBoxesKey = "cloud.max_boxes"

// Config is application.PauseConfig over the home's config.toml, changed the
// way mw config set changes it: a dated backup first, and its words (the way
// back among them) written to Out.
type Config struct {
	Path string
	Out  io.Writer
}

var _ application.PauseConfig = Config{}

// MaxBoxes implements application.PauseConfig.
func (c Config) MaxBoxes(context.Context) (string, bool, error) {
	data, _, err := configfile.New(c.Path).Read()
	if err != nil {
		return "", false, err
	}
	file, err := domain.ParseConfigTOML(string(data))
	if err != nil {
		return "", false, err
	}
	table, key, err := domain.SplitConfigKey(MaxBoxesKey)
	if err != nil {
		return "", false, err
	}
	literal, ok := file.Lookup(table, key)
	if !ok {
		return "", false, nil
	}
	value, _ := domain.TOMLString(literal)
	return value, true, nil
}

// SetMaxBoxes implements application.PauseConfig. A value the key already
// holds is not a fault.
func (c Config) SetMaxBoxes(ctx context.Context, value string) error {
	out := c.Out
	if out == nil {
		out = io.Discard
	}
	err := application.ConfigSet{File: configfile.New(c.Path), Out: out}.Run(ctx, MaxBoxesKey, value, false)
	if err != nil && strings.Contains(err.Error(), "is unchanged") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", MaxBoxesKey, err)
	}
	return nil
}
