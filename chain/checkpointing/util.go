package checkpointing

import (
	"context"
	"fmt"

	"github.com/Zondax/multi-party-sig/pkg/protocol"
)

// HandlerLoop blocks until the handler has finished. The result of the execution is given by Handler.Result().
func HandlerLoop(ctx context.Context, h protocol.Handler, network *Network) {
	for {
		msg, ok := <-h.Listen()
		fmt.Println("Outgoing message:", msg)
		fmt.Println(ok)
		if !ok {
			network.Done()
			// the channel was closed, indicating that the protocol is done executing.
			fmt.Println("Should be good")
			return
		}
		network.Send(ctx, msg)

		for _, _ = range network.Parties() {
			msg = network.Next(ctx)
			fmt.Println("Incoming message:", msg)
			h.Accept(msg)
		}
	}

}
