package api_test

import (
	"testing"

	apicmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCommandStructureDeep(t *testing.T) {
	cmd := apicmd.NewCommand()

	require.NotNil(t, cmd)
	assert.Equal(t, "web", cmd.Use, "command Use should be 'web'")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE, "RunE should be set")

	// Verify flags beyond what's tested in TestNewCommand_Structure
	hostFlag := cmd.Flags().Lookup("host")
	require.NotNil(t, hostFlag, "host flag should be registered")

	portFlag := cmd.Flags().Lookup("port")
	require.NotNil(t, portFlag, "port flag should be registered")
}
