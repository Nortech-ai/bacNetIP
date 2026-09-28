package bacnet

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/Nortech-ai/bacNetIP/btypes"
	"github.com/Nortech-ai/bacNetIP/datalink"
	"github.com/Nortech-ai/bacNetIP/encoding"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestHandleMsgUnconfirmedDoesNotError(t *testing.T) {
	cases := []struct {
		name    string
		service btypes.ServiceUnconfirmed
	}{
		{name: "COVNotification", service: btypes.ServiceUnconfirmedCOVNotification},
		{name: "TimeSynchronization", service: btypes.ServiceUnconfirmedTimeSync},
		{name: "UTCTimeSynchronization", service: btypes.ServiceUnconfirmedUTCTimeSync},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			link := datalink.NewMockDataLink()
			cli, err := NewClient(&ClientBuilder{
				DataLink: link,
				Ip:       "192.168.1.100",
			})
			require.NoError(t, err)
			defer cli.Close()

			c := cli.(*client)
			var buf bytes.Buffer
			c.log.SetOutput(&buf)
			c.log.SetLevel(log.DebugLevel)
			c.log.SetFormatter(&log.TextFormatter{
				DisableColors:    true,
				DisableTimestamp: true,
			})

			src := &btypes.Address{Mac: []byte{192, 168, 1, 10, 0xBA, 0xC0}}
			c.handleMsg(src, unconfirmedServicePacket(t, tc.service))

			out := buf.String()
			assert.NotContains(t, out, "level=error")
			assert.Contains(t, out, fmt.Sprintf("Ignoring unconfirmed service %d", tc.service))
		})
	}
}

func unconfirmedServicePacket(t *testing.T, service btypes.ServiceUnconfirmed) []byte {
	t.Helper()

	payload := encoding.NewEncoder()
	payload.NPDU(&btypes.NPDU{
		Version: btypes.ProtocolVersion,
	})
	err := payload.APDU(btypes.APDU{
		DataType:           btypes.UnconfirmedServiceRequest,
		UnconfirmedService: service,
	})
	require.NoError(t, err)
	require.NoError(t, payload.Error())

	data := payload.Bytes()
	enc := encoding.NewEncoder()
	err = enc.BVLC(btypes.BVLC{
		Type:     btypes.BVLCTypeBacnetIP,
		Function: btypes.BacFuncBroadcast,
		Length:   4 + uint16(len(data)),
		Data:     data,
	})
	require.NoError(t, err)
	require.NoError(t, enc.Error())
	return enc.Bytes()
}
