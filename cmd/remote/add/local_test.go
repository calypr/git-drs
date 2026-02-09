package add

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddLocalRemote(t *testing.T) {
	assert.NotNil(t, LocalCmd)
	assert.Equal(t, "local <remote-name> <url>", LocalCmd.Use)
}
