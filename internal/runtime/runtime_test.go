package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestControllerAcquireRelease(t *testing.T) {
	limits := NewLimits(1, 1)
	controller := NewController(limits)

	require.Equal(t, limits, controller.LimitsSnapshot())

	require.NoError(t, controller.AcquireRequest(context.Background()))
	controller.ReleaseRequest()

	require.NoError(t, controller.AcquireWorkbook(context.Background()))
	controller.ReleaseWorkbook()
}
