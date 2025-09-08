package bacnet

import (
	"fmt"
	"io"
	"testing"

	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/stretchr/testify/assert"
)

func TestClientRunEOF(t *testing.T) {
	link := datalink.NewMockDataLink()
	cb := &ClientBuilder{
		DataLink: link,
		Ip:       "192.168.1.100",
	}

	c, err := NewClient(cb)
	assert.NoError(t, err)
	defer c.Close()

	link.Received = []datalink.ReceivedMessage{
		{
			Error: fmt.Errorf("Hey! I'm an error!"),
		},
		{
			Error: io.EOF,
		},
	}

	// This should only exit if we get an EOF error
	c.ClientRun()

	// So if we got here the test succeeded. This tests that we both handled the first
	// error and continued processing messages, and that we exited the when we got an EOF
}
