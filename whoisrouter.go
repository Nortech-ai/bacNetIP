package bacnet

import (
	"fmt"

	"github.com/ytuox/bacnet/btypes"
	"github.com/ytuox/bacnet/btypes/ndpu"
	"github.com/ytuox/bacnet/encoding"
)

/*
Is in beta
*/

func (c *client) WhoIsRouterToNetwork() (resp *[]btypes.Address) {
	var err error
	dest := *c.dataLink.GetBroadcastAddress()
	enc := encoding.NewEncoder()
	npdu := &btypes.NPDU{
		Version:                 btypes.ProtocolVersion,
		Destination:             &dest,
		Source:                  c.dataLink.GetMyAddress(),
		IsNetworkLayerMessage:   true,
		NetworkLayerMessageType: ndpu.WhoIsRouterToNetwork,
		// We are not expecting a direct reply from a single destination
		ExpectingReply: false,
		Priority:       btypes.Normal,
		HopCount:       btypes.DefaultHopCount,
	}
	enc.NPDU(npdu)
	// Run in parallel
	errChan := make(chan error)
	broadcast := &SetBroadcastType{Set: true, BacFunc: btypes.BacFuncBroadcast}
	go func() {
		_, err = c.Send(dest, npdu, enc.Bytes(), broadcast)
		errChan <- err
	}()
	values, err := c.utsm.Subscribe(1, 65534) //65534 is the max number a network can be
	if err != nil {
		fmt.Println(`err`, err)
		return
	}
	err = <-errChan
	if err != nil {
		return
	}
	var list []btypes.Address
	for _, addresses := range values {
		r, ok := addresses.([]btypes.Address)
		if !ok {
			continue
		}
		list = append(list, r...)
	}
	return &list

}
