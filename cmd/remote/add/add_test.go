package add

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddCmd(t *testing.T) {
	assert.Equal(t, "add", Cmd.Use)
	assert.NotEmpty(t, Cmd.Short)
}

func TestGen3Cmd(t *testing.T) {
	assert.Equal(t, "gen3 [remote-name]", Gen3Cmd.Use)
}

func TestAnvilCmd(t *testing.T) {
	assert.Equal(t, "anvil [remote-name]", AnvilCmd.Use)
}
