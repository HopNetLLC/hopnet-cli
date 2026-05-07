package bridge

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/HopNetLLC/hopnet-cli/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRunRequiresRoute(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "c.json"))
	require.NoError(t, err)
	require.Error(t, Run(cfg, Options{Listen: "127.0.0.1:0"}))
}

func TestRunRequiresListen(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "c.json"))
	require.NoError(t, err)
	require.Error(t, Run(cfg, Options{RouteID: "rt_x"}))
}

func TestRunReturnsNotImplementedWhenRouteExists(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "c.json"))
	require.NoError(t, err)
	cfg.PutRoute("rt_x", config.Route{Token: "rtk_x"})
	err = Run(cfg, Options{RouteID: "rt_x", Listen: "127.0.0.1:0"})
	require.ErrorIs(t, err, ErrNotImplemented)
}

func TestRunUnknownRouteIsClearer(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "c.json"))
	require.NoError(t, err)
	err = Run(cfg, Options{RouteID: "rt_unknown", Listen: "127.0.0.1:0"})
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrNotImplemented))
}
